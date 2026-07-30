package bip322

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	addressv2 "github.com/btcsuite/btcd/address/v2"
	upstream "github.com/btcsuite/btcd/bip322"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcutil"
	chaincfgv2 "github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	psbtv2 "github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript"
	txscriptv2 "github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire"
	wirev2 "github.com/btcsuite/btcd/wire/v2"
	"github.com/lightninglabs/loop/staticaddr/script"
	"github.com/lightningnetwork/lnd/input"
)

// Plan is an immutable BIP-322 signing plan. Call SigningRequest to obtain a
// detached transaction for an external signer, then Finalize with the returned
// signatures.
type Plan struct {
	// packet is the immutable PSBT v2 signing template.
	packet *psbtv2.Packet

	// staticAddress constructs the timeout-path witness.
	staticAddress *script.StaticAddress

	// clientPubKey is an immutable x-only copy used for post-signing checks.
	clientPubKey []byte

	// message is retained for final BIP-322 verification.
	message string

	// address is the encoded address returned with the signature.
	address string

	// network supplies the upstream verifier's Bech32 prefix.
	network *chaincfgv2.Params

	// expiry is the exact CSV constraint expected from verification.
	expiry uint32

	// coins are the real proof inputs in signed order.
	coins []Coin
}

// PrepareFull constructs a full BIP-322 signing plan for the virtual challenge
// input.
func PrepareFull(message string, params AddressParameters) (*Plan, error) {
	return prepare(message, params, nil)
}

// PrepareProof constructs a proof-of-funds BIP-322 signing plan. Coins are
// validated and placed after the mandatory challenge input in BIP-69 order.
func PrepareProof(message string, params AddressParameters,
	coins []Coin) (*Plan, error) {

	switch {
	case len(coins) == 0:
		return nil, fmt.Errorf("%w: proof selection is empty",
			ErrInvalidSelection)

	case len(coins) > MaxProofDeposits:
		return nil, fmt.Errorf("%w: got %d, max %d",
			ErrTooManyDeposits, len(coins), MaxProofDeposits)
	}

	coins = cloneCoins(coins)
	outpoints := make([]wire.OutPoint, len(coins))
	coinByOutpoint := make(map[wire.OutPoint]Coin, len(coins))

	// Validate and index before sorting so malformed or duplicate inputs
	// cannot be hidden by canonicalization.
	for idx, coin := range coins {
		if err := validateOutpoint(coin.OutPoint); err != nil {
			return nil, err
		}
		if err := validateCoin(params.PkScript, coin); err != nil {
			return nil, err
		}
		if _, ok := coinByOutpoint[coin.OutPoint]; ok {
			return nil, fmt.Errorf("duplicate proof coin %v",
				coin.OutPoint)
		}

		outpoints[idx] = coin.OutPoint
		coinByOutpoint[coin.OutPoint] = coin
	}

	sortOutpointsBIP69(outpoints)

	// Rebuild the caller-independent coin slice in signed input order.
	for idx, outpoint := range outpoints {
		coins[idx] = coinByOutpoint[outpoint]
	}

	return prepare(message, params, coins)
}

// prepare builds the shared full-signature template. Proof coins, when
// present, are appended after BIP-322's mandatory virtual challenge input.
func prepare(message string, params AddressParameters,
	coins []Coin) (*Plan, error) {

	if err := validateMessage(message); err != nil {
		return nil, err
	}

	staticAddress, address, network, err := validateAddress(params)
	if err != nil {
		return nil, err
	}

	pkScript := bytes.Clone(params.PkScript)
	packet, err := upstream.BuildToSignPacketFull(
		[]byte(message), pkScript, 2, 0, params.Expiry,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to build BIP-322 packet: %w", err)
	}

	// Proof-of-funds inputs spend real coins without changing the virtual
	// challenge input at index zero.
	for _, coin := range coins {
		var hash chainhash.Hash
		copy(hash[:], coin.OutPoint.Hash[:])

		packet.UnsignedTx.AddTxIn(&wirev2.TxIn{
			PreviousOutPoint: wirev2.OutPoint{
				Hash:  hash,
				Index: coin.OutPoint.Index,
			},
			Sequence: params.Expiry,
		})
		packet.Inputs = append(packet.Inputs, *psbtv2.NewPsbtInput(
			nil, &wirev2.TxOut{
				Value:    int64(coin.Amount),
				PkScript: bytes.Clone(pkScript),
			},
		))
	}

	return &Plan{
		packet:        packet,
		staticAddress: staticAddress,
		clientPubKey:  schnorr.SerializePubKey(params.ClientPubKey),
		message:       message,
		address:       address,
		network:       network,
		expiry:        params.Expiry,
		coins:         cloneCoins(coins),
	}, nil
}

// SigningRequest returns a detached v1 wire transaction and the full prevout
// and tapscript context. Mutating the result cannot change the plan.
func (p *Plan) SigningRequest() (*SigningRequest, error) {
	if p == nil || p.packet == nil || p.packet.UnsignedTx == nil {
		return nil, fmt.Errorf("BIP-322 plan is nil")
	}

	// lnd signs wire v1 transactions, while the upstream BIP-322 package
	// builds PSBT v2 transactions.
	tx, err := wireV1Tx(p.packet.UnsignedTx)
	if err != nil {
		return nil, err
	}

	request := &SigningRequest{
		Tx:          tx,
		PrevOutputs: make([]*wire.TxOut, len(p.packet.Inputs)),
		Inputs:      make([]SigningInput, len(p.packet.Inputs)),
	}

	// Copy every sighash component so callers cannot mutate the plan across
	// the signing boundary.
	for idx, packetInput := range p.packet.Inputs {
		if packetInput.WitnessUtxo == nil {
			return nil, fmt.Errorf("input %d is missing witness UTXO", idx)
		}

		output := wire.NewTxOut(
			packetInput.WitnessUtxo.Value,
			bytes.Clone(packetInput.WitnessUtxo.PkScript),
		)
		request.PrevOutputs[idx] = output
		request.Inputs[idx] = SigningInput{
			InputIndex: idx,
			Output: wire.NewTxOut(
				output.Value, bytes.Clone(output.PkScript),
			),
			WitnessScript: bytes.Clone(
				p.staticAddress.TimeoutLeaf.Script,
			),
		}
	}

	return request, nil
}

// Finalize verifies every raw Schnorr signature against the exact transaction,
// prevouts, input index, timeout leaf and SIGHASH_DEFAULT committed by the
// plan. Only then does it construct and locally verify the BIP-322 payload.
func (p *Plan) Finalize(rawSignatures [][]byte) (*Result, error) {
	if p == nil || p.packet == nil || p.staticAddress == nil {
		return nil, fmt.Errorf("BIP-322 plan is nil")
	}

	// Finalization works on a copy so a failed signer response cannot poison
	// a retry of the same plan.
	packet, err := p.packet.Copy()
	if err != nil {
		return nil, fmt.Errorf("unable to copy BIP-322 packet: %w", err)
	}
	if len(rawSignatures) != len(packet.Inputs) {
		return nil, fmt.Errorf("signer returned %d signatures, expected %d",
			len(rawSignatures), len(packet.Inputs))
	}

	clientPubKey, err := schnorr.ParsePubKey(p.clientPubKey)
	if err != nil {
		return nil, fmt.Errorf("unable to parse client public key: %w", err)
	}

	// Taproot commits to all prevout scripts and amounts. Recompute the
	// sighash from the plan instead of trusting signer-returned context.
	prevOutFetcher := upstream.PsbtPrevOutputFetcher(packet)
	sigHashes := txscriptv2.NewTxSigHashes(
		packet.UnsignedTx, prevOutFetcher,
	)
	timeoutLeaf := txscriptv2.NewBaseTapLeaf(
		p.staticAddress.TimeoutLeaf.Script,
	)

	for idx, rawSignature := range rawSignatures {
		if len(rawSignature) != schnorr.SignatureSize {
			return nil, fmt.Errorf("input %d signature has length %d, "+
				"expected %d", idx, len(rawSignature),
				schnorr.SignatureSize)
		}

		sigHash, err := txscriptv2.CalcTapscriptSignaturehash(
			sigHashes, txscriptv2.SigHashDefault,
			packet.UnsignedTx, idx, prevOutFetcher, timeoutLeaf,
		)
		if err != nil {
			return nil, fmt.Errorf("unable to calculate input %d "+
				"sighash: %w", idx, err)
		}

		signature, err := schnorr.ParseSignature(rawSignature)
		if err != nil {
			return nil, fmt.Errorf("unable to parse input %d "+
				"signature: %w", idx, err)
		}
		if !signature.Verify(sigHash, clientPubKey) {
			return nil, fmt.Errorf("signer returned invalid signature "+
				"for input %d", idx)
		}

		// A custom tapscript leaf is not finalized by the generic PSBT
		// finalizer, so encode the complete witness explicitly.
		witness, err := p.staticAddress.GenTimeoutWitness(rawSignature)
		if err != nil {
			return nil, fmt.Errorf("unable to construct input %d "+
				"witness: %w", idx, err)
		}

		var witnessBytes bytes.Buffer
		witnessV2 := make(wirev2.TxWitness, len(witness))
		for witnessIdx := range witness {
			witnessV2[witnessIdx] = bytes.Clone(witness[witnessIdx])
		}
		if err := psbtv2.WriteTxWitness(
			&witnessBytes, witnessV2,
		); err != nil {
			return nil, fmt.Errorf("unable to finalize input %d: %w",
				idx, err)
		}
		packet.Inputs[idx].FinalScriptWitness = witnessBytes.Bytes()
	}

	serialized, err := upstream.SerializeSignature(packet)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize BIP-322 signature: %w",
			err)
	}

	// The presence of real inputs determines the serialized BIP-322 variant.
	expectedPrefix := upstream.PrefixFull
	if len(p.coins) > 0 {
		expectedPrefix = upstream.PrefixProofOfFunds
	}
	if !strings.HasPrefix(serialized, expectedPrefix) {
		return nil, fmt.Errorf("unexpected BIP-322 signature prefix")
	}

	// Full verification catches witness or serialization mistakes after the
	// individual Schnorr checks and extracts the actual time constraints.
	constraints, err := p.verify(serialized)
	if err != nil {
		return nil, err
	}

	result := &Result{
		Address:     p.address,
		Signature:   serialized,
		Constrained: constraints.Constrained,
		ValidAtTime: constraints.ValidAtTime,
		ValidAtAge:  constraints.ValidAtAge,
		Coins:       cloneCoins(p.coins),
	}

	// Sum only after the proof is valid and reject signed integer overflow.
	for _, coin := range p.coins {
		if int64(coin.Amount) > math.MaxInt64-result.TotalAmountSat {
			return nil, fmt.Errorf("proof amount overflows int64")
		}
		result.TotalAmountSat += int64(coin.Amount)
	}

	return result, nil
}

// verify checks the completed generic BIP-322 payload and its exact CSV
// constraint. It is a signer safety check, not a public verification API.
func (p *Plan) verify(signature string) (upstream.TimeConstraints, error) {
	address, err := addressv2.NewAddressTaproot(
		p.packet.Inputs[0].WitnessUtxo.PkScript[2:], p.network,
	)
	if err != nil {
		return upstream.TimeConstraints{}, err
	}

	valid, constraints, err := upstream.VerifyMessage(
		p.message, address, signature,
	)
	if err != nil {
		return upstream.TimeConstraints{}, fmt.Errorf(
			"local BIP-322 verification failed: %w", err,
		)
	}
	if !valid {
		return upstream.TimeConstraints{}, fmt.Errorf(
			"local BIP-322 verification returned invalid",
		)
	}
	if !constraints.Constrained ||
		constraints.ValidAtTime != 0 ||
		constraints.ValidAtAge != p.expiry {

		return upstream.TimeConstraints{}, fmt.Errorf(
			"unexpected BIP-322 time constraints: %+v", constraints,
		)
	}

	return constraints, nil
}

// validateMessage enforces the RPC-compatible UTF-8 and size limits.
func validateMessage(message string) error {
	switch {
	case !utf8.ValidString(message):
		return fmt.Errorf("message must be valid UTF-8")

	case len(message) > MaxMessageBytes:
		return fmt.Errorf("message is too large: got %d bytes, max %d",
			len(message), MaxMessageBytes)

	default:
		return nil
	}
}

// validateAddress reconstructs the static address from persisted parameters
// and rejects any script that does not match that construction.
func validateAddress(params AddressParameters) (*script.StaticAddress, string,
	*chaincfgv2.Params, error) {

	switch {
	case params.ClientPubKey == nil:
		return nil, "", nil, fmt.Errorf(
			"static address client public key is nil",
		)

	case params.ServerPubKey == nil:
		return nil, "", nil, fmt.Errorf(
			"static address server public key is nil",
		)

	case params.ChainParams == nil:
		return nil, "", nil, fmt.Errorf("chain parameters are nil")

	case !params.ProtocolVersion.Valid():
		return nil, "", nil, fmt.Errorf(
			"unsupported static address protocol version %v",
			params.ProtocolVersion,
		)

	case !txscript.IsPayToTaproot(params.PkScript) ||
		len(params.PkScript) != 34:

		return nil, "", nil, fmt.Errorf(
			"static address script is not P2TR",
		)
	}
	if err := script.ValidateExpiry(params.Expiry); err != nil {
		return nil, "", nil, err
	}

	staticAddress, err := script.NewStaticAddress(
		input.MuSig2Version100RC2, int64(params.Expiry),
		params.ClientPubKey, params.ServerPubKey,
	)
	if err != nil {
		return nil, "", nil, err
	}
	expectedPkScript, err := staticAddress.StaticAddressScript()
	if err != nil {
		return nil, "", nil, err
	}
	if !bytes.Equal(expectedPkScript, params.PkScript) {
		return nil, "", nil, fmt.Errorf(
			"persisted static address script does not match parameters",
		)
	}

	address, err := btcutil.NewAddressTaproot(
		params.PkScript[2:], params.ChainParams,
	)
	if err != nil {
		return nil, "", nil, err
	}

	// The v2 address API currently reads only Bech32HRPSegwit when encoding
	// and decoding Taproot addresses. Keep this minimal adapter explicit so
	// an upstream expansion of network validation is easy to notice here.
	network := &chaincfgv2.Params{
		Bech32HRPSegwit: params.ChainParams.Bech32HRPSegwit,
	}

	return staticAddress, address.String(), network, nil
}

// wireV1Tx losslessly converts a wire v2 transaction through consensus
// serialization so lnd can sign it without a hand-maintained field mapping.
func wireV1Tx(tx *wirev2.MsgTx) (*wire.MsgTx, error) {
	var serialized bytes.Buffer
	if err := tx.Serialize(&serialized); err != nil {
		return nil, fmt.Errorf("unable to serialize v2 transaction: %w",
			err)
	}

	var converted wire.MsgTx
	if err := converted.Deserialize(bytes.NewReader(
		serialized.Bytes(),
	)); err != nil {
		return nil, fmt.Errorf("unable to deserialize v1 transaction: %w",
			err)
	}

	return &converted, nil
}

// cloneCoins deep-copies coins and their mutable scripts.
func cloneCoins(coins []Coin) []Coin {
	result := slices.Clone(coins)
	for idx := range result {
		result[idx] = cloneCoin(result[idx])
	}

	return result
}
