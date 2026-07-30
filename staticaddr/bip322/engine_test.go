package bip322

import (
	"bytes"
	"strings"
	"testing"

	addressv2 "github.com/btcsuite/btcd/address/v2"
	upstream "github.com/btcsuite/btcd/bip322"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	chaincfgv2 "github.com/btcsuite/btcd/chaincfg/v2"
	psbtv2 "github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	wirev2 "github.com/btcsuite/btcd/wire/v2"
	"github.com/lightninglabs/loop/staticaddr/script"
	"github.com/lightninglabs/loop/staticaddr/version"
	"github.com/lightningnetwork/lnd/input"
	"github.com/stretchr/testify/require"
)

// engineHarness contains a valid static-address construction and its client
// signing key.
type engineHarness struct {
	// clientKey signs every timeout-path input.
	clientKey *btcec.PrivateKey

	// staticAddress supplies the expected timeout leaf and control block.
	staticAddress *script.StaticAddress

	// params is a valid baseline mutated by validation tests.
	params AddressParameters
}

// newEngineHarness creates a valid regtest static-address signing fixture.
func newEngineHarness(t *testing.T) *engineHarness {
	t.Helper()

	clientKey, err := btcec.NewPrivateKey()
	require.NoError(t, err)
	serverKey, err := btcec.NewPrivateKey()
	require.NoError(t, err)

	const expiry = uint32(144)
	staticAddress, err := script.NewStaticAddress(
		input.MuSig2Version100RC2, int64(expiry), clientKey.PubKey(),
		serverKey.PubKey(),
	)
	require.NoError(t, err)
	pkScript, err := staticAddress.StaticAddressScript()
	require.NoError(t, err)

	return &engineHarness{
		clientKey:     clientKey,
		staticAddress: staticAddress,
		params: AddressParameters{
			ClientPubKey:    clientKey.PubKey(),
			ServerPubKey:    serverKey.PubKey(),
			Expiry:          expiry,
			PkScript:        pkScript,
			ProtocolVersion: version.ProtocolVersion_V0,
			ChainParams:     &chaincfg.RegressionNetParams,
		},
	}
}

// signerMutation selects one part of the signing context to corrupt.
type signerMutation struct {
	// corruptSignature flips a bit after signing.
	corruptSignature bool

	// prevOutputDelta changes the amount committed by input zero.
	prevOutputDelta int64

	// wrongLeaf signs a different tapscript leaf.
	wrongLeaf bool

	// wrongInputIndex signs proof inputs as input zero.
	wrongInputIndex bool

	// wrongSigHashType appends a non-default sighash byte.
	wrongSigHashType bool
}

// signPlan emulates lnd signing and optionally corrupts its signing context.
func signPlan(t *testing.T, plan *Plan, key *btcec.PrivateKey,
	mutation signerMutation) [][]byte {

	t.Helper()

	request, err := plan.SigningRequest()
	require.NoError(t, err)

	signingPrevOutputs := make([]*wire.TxOut, len(request.PrevOutputs))
	prevOutFetcher := txscript.NewMultiPrevOutFetcher(nil)

	// Build the exact detached prevout set that the signer uses.
	for idx, txIn := range request.Tx.TxIn {
		signingPrevOutputs[idx] = wire.NewTxOut(
			request.PrevOutputs[idx].Value,
			bytes.Clone(request.PrevOutputs[idx].PkScript),
		)
		if idx == 0 {
			signingPrevOutputs[idx].Value += mutation.prevOutputDelta
		}
		prevOutFetcher.AddPrevOut(
			txIn.PreviousOutPoint, signingPrevOutputs[idx],
		)
	}
	sigHashes := txscript.NewTxSigHashes(request.Tx, prevOutFetcher)

	signatures := make([][]byte, len(request.Inputs))

	// Sign each requested tapscript input independently, as lnd does.
	for idx, signingInput := range request.Inputs {
		leafScript := signingInput.WitnessScript
		if mutation.wrongLeaf {
			leafScript = []byte{txscript.OP_TRUE}
		}
		leaf := txscript.NewBaseTapLeaf(leafScript)

		signingIndex := idx
		if mutation.wrongInputIndex && idx > 0 {
			signingIndex = 0
		}

		hashType := txscript.SigHashDefault
		if mutation.wrongSigHashType {
			hashType = txscript.SigHashAll
		}

		signature, err := txscript.RawTxInTapscriptSignature(
			request.Tx, sigHashes, signingIndex,
			signingPrevOutputs[signingIndex].Value,
			signingPrevOutputs[signingIndex].PkScript, leaf, hashType,
			key,
		)
		require.NoError(t, err)
		signatures[idx] = signature
	}

	if mutation.corruptSignature {
		signatures[0][0] ^= 1
	}

	return signatures
}

// TestPrepareAndFinalizeFull proves that a full timeout-path signature is
// constrained by the stored CSV expiry and accepted by the generic verifier.
func TestPrepareAndFinalizeFull(t *testing.T) {
	t.Parallel()

	harness := newEngineHarness(t)
	plan, err := PrepareFull("loop full proof", harness.params)
	require.NoError(t, err)

	request, err := plan.SigningRequest()
	require.NoError(t, err)
	require.Len(t, request.Tx.TxIn, 1)
	require.EqualValues(t, 2, request.Tx.Version)
	require.Equal(t, harness.params.Expiry, request.Tx.TxIn[0].Sequence)
	require.Equal(
		t, harness.staticAddress.TimeoutLeaf.Script,
		request.Inputs[0].WitnessScript,
	)

	result, err := plan.Finalize(signPlan(
		t, plan, harness.clientKey, signerMutation{},
	))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(
		result.Signature, upstream.PrefixFull,
	))
	require.True(t, result.Constrained)
	require.Zero(t, result.ValidAtTime)
	require.Equal(t, harness.params.Expiry, result.ValidAtAge)
	require.Empty(t, result.Coins)
	require.Zero(t, result.TotalAmountSat)

	// Verify interoperability independently of the Plan's post-signing check.
	network := &chaincfgv2.Params{
		Bech32HRPSegwit: chaincfg.RegressionNetParams.Bech32HRPSegwit,
	}
	address, err := addressv2.NewAddressTaproot(
		harness.params.PkScript[2:], network,
	)
	require.NoError(t, err)
	valid, constraints, err := upstream.VerifyMessage(
		"loop full proof", address, result.Signature,
	)
	require.NoError(t, err)
	require.True(t, valid)
	require.Equal(t, harness.params.Expiry, constraints.ValidAtAge)
}

// TestFinalizeRejectsInvalidSigningContext proves that Finalize binds every
// signature to the exact prevout, leaf, input, and sighash type.
func TestFinalizeRejectsInvalidSigningContext(t *testing.T) {
	t.Parallel()

	const invalidSignatureErr = "invalid signature for input 0"

	tests := []struct {
		name     string
		mutation signerMutation
		error    string
	}{
		{
			name: "corrupt signature",
			mutation: signerMutation{
				corruptSignature: true,
			},
			error: invalidSignatureErr,
		},
		{
			name: "wrong prevout amount",
			mutation: signerMutation{
				prevOutputDelta: 1,
			},
			error: invalidSignatureErr,
		},
		{
			name: "wrong tapscript leaf",
			mutation: signerMutation{
				wrongLeaf: true,
			},
			error: invalidSignatureErr,
		},
		{
			name: "wrong sighash type",
			mutation: signerMutation{
				wrongSigHashType: true,
			},
			error: "signature has length 65",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			harness := newEngineHarness(t)
			plan, err := PrepareFull("signing boundary", harness.params)
			require.NoError(t, err)

			_, err = plan.Finalize(signPlan(
				t, plan, harness.clientKey, test.mutation,
			))
			require.ErrorContains(t, err, test.error)
		})
	}
}

// TestPrepareAndFinalizeProofOfFunds checks canonical real-input ordering,
// amount totals, the pof prefix, and per-input signing.
func TestPrepareAndFinalizeProofOfFunds(t *testing.T) {
	t.Parallel()

	harness := newEngineHarness(t)
	first := Coin{
		OutPoint: wire.OutPoint{
			Hash:  chainhash.Hash{2},
			Index: 4,
		},
		Amount:        20_000,
		Confirmations: 2,
		PkScript:      harness.params.PkScript,
	}
	second := Coin{
		OutPoint: wire.OutPoint{
			Hash:  chainhash.Hash{1},
			Index: 7,
		},
		Amount:        30_000,
		Confirmations: 3,
		PkScript:      harness.params.PkScript,
	}

	plan, err := PrepareProof(
		"loop funds proof", harness.params, []Coin{first, second},
	)
	require.NoError(t, err)
	request, err := plan.SigningRequest()
	require.NoError(t, err)
	require.Len(t, request.Tx.TxIn, 3)
	require.Equal(t, second.OutPoint, request.Tx.TxIn[1].PreviousOutPoint)
	require.Equal(t, first.OutPoint, request.Tx.TxIn[2].PreviousOutPoint)

	result, err := plan.Finalize(signPlan(
		t, plan, harness.clientKey, signerMutation{},
	))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(
		result.Signature, upstream.PrefixProofOfFunds,
	))
	require.Equal(t, int64(50_000), result.TotalAmountSat)
	require.Equal(t, []Coin{second, first}, result.Coins)

	_, err = plan.Finalize(signPlan(
		t, plan, harness.clientKey, signerMutation{
			wrongInputIndex: true,
		},
	))
	require.ErrorContains(t, err, "invalid signature for input 1")
}

// TestPlanBoundaryValidation checks signature cardinality, fixed signature
// length, nil receivers, and defensive copies returned to the signer.
func TestPlanBoundaryValidation(t *testing.T) {
	t.Parallel()

	harness := newEngineHarness(t)
	plan, err := PrepareFull("boundary", harness.params)
	require.NoError(t, err)

	_, err = plan.Finalize(nil)
	require.ErrorContains(t, err, "returned 0 signatures, expected 1")

	signatures := signPlan(t, plan, harness.clientKey, signerMutation{})
	signatures[0] = signatures[0][:63]
	_, err = plan.Finalize(signatures)
	require.ErrorContains(t, err, "signature has length 63")

	firstRequest, err := plan.SigningRequest()
	require.NoError(t, err)
	firstRequest.Tx.Version = 99
	firstRequest.PrevOutputs[0].Value = 99
	firstRequest.Inputs[0].WitnessScript[0] ^= 1
	harness.params.PkScript[0] ^= 1

	secondRequest, err := plan.SigningRequest()
	require.NoError(t, err)
	require.EqualValues(t, 2, secondRequest.Tx.Version)
	require.Zero(t, secondRequest.PrevOutputs[0].Value)
	require.Equal(
		t, harness.staticAddress.TimeoutLeaf.Script,
		secondRequest.Inputs[0].WitnessScript,
	)

	var nilPlan *Plan
	_, err = nilPlan.SigningRequest()
	require.ErrorContains(t, err, "plan is nil")
	_, err = nilPlan.Finalize(nil)
	require.ErrorContains(t, err, "plan is nil")
}

// TestPrepareValidation covers message, address, expiry, and proof-coin
// validation performed before the wallet is asked to sign.
func TestPrepareValidation(t *testing.T) {
	t.Parallel()

	harness := newEngineHarness(t)
	validCoin := Coin{
		OutPoint: wire.OutPoint{
			Hash:  chainhash.Hash{1},
			Index: 1,
		},
		Amount:        10_000,
		Confirmations: 1,
		PkScript:      harness.params.PkScript,
	}

	tests := []struct {
		name    string
		message string
		mutate  func(*AddressParameters)
		error   string
	}{
		{
			name:    "invalid UTF-8",
			message: string([]byte{0xff}),
			error:   "valid UTF-8",
		},
		{
			name:    "message too large",
			message: strings.Repeat("a", MaxMessageBytes+1),
			error:   "message is too large",
		},
		{
			name: "nil client key",
			mutate: func(params *AddressParameters) {
				params.ClientPubKey = nil
			},
			error: "client public key is nil",
		},
		{
			name: "nil server key",
			mutate: func(params *AddressParameters) {
				params.ServerPubKey = nil
			},
			error: "server public key is nil",
		},
		{
			name: "nil network",
			mutate: func(params *AddressParameters) {
				params.ChainParams = nil
			},
			error: "chain parameters are nil",
		},
		{
			name: "unsupported protocol",
			mutate: func(params *AddressParameters) {
				params.ProtocolVersion = 100
			},
			error: "unsupported static address protocol",
		},
		{
			name: "zero expiry",
			mutate: func(params *AddressParameters) {
				params.Expiry = 0
			},
			error: "must be non-zero",
		},
		{
			name: "not P2TR",
			mutate: func(params *AddressParameters) {
				params.PkScript = []byte{txscript.OP_TRUE}
			},
			error: "script is not P2TR",
		},
		{
			name: "script mismatch",
			mutate: func(params *AddressParameters) {
				params.PkScript = bytes.Clone(params.PkScript)
				params.PkScript[len(params.PkScript)-1] ^= 1
			},
			error: "does not match parameters",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			params := harness.params
			params.PkScript = bytes.Clone(params.PkScript)
			if test.mutate != nil {
				test.mutate(&params)
			}

			message := test.message
			if message == "" {
				message = "message"
			}
			_, err := PrepareFull(message, params)
			require.ErrorContains(t, err, test.error)
		})
	}

	_, err := PrepareProof("message", harness.params, nil)
	require.ErrorContains(t, err, "selection is empty")
	require.ErrorIs(t, err, ErrInvalidSelection)

	tooMany := make([]Coin, MaxProofDeposits+1)
	_, err = PrepareProof("message", harness.params, tooMany)
	require.ErrorContains(t, err, "too many proof deposits")
	require.ErrorIs(t, err, ErrTooManyDeposits)

	_, err = PrepareProof(
		"message", harness.params, []Coin{validCoin, validCoin},
	)
	require.ErrorContains(t, err, "duplicate proof coin")

	invalidCoin := validCoin
	invalidCoin.Confirmations = 0
	_, err = PrepareProof("message", harness.params, []Coin{invalidCoin})
	require.ErrorContains(t, err, "unconfirmed")
}

// TestTimeoutPathSimpleIsInvalid documents why static addresses cannot produce
// BIP-322 simple signatures: simple uses transaction version zero, while CSV
// requires version two or later.
func TestTimeoutPathSimpleIsInvalid(t *testing.T) {
	t.Parallel()

	harness := newEngineHarness(t)
	const message = "simple cannot satisfy CSV"
	packet, err := upstream.BuildToSignPacketSimple(
		[]byte(message), harness.params.PkScript,
	)
	require.NoError(t, err)

	tx, err := wireV1Tx(packet.UnsignedTx)
	require.NoError(t, err)
	prevOutput := wire.NewTxOut(
		packet.Inputs[0].WitnessUtxo.Value,
		packet.Inputs[0].WitnessUtxo.PkScript,
	)
	prevOutFetcher := txscript.NewMultiPrevOutFetcher(nil)
	prevOutFetcher.AddPrevOut(tx.TxIn[0].PreviousOutPoint, prevOutput)
	sigHashes := txscript.NewTxSigHashes(tx, prevOutFetcher)

	// A valid Schnorr signature cannot make the version-zero transaction
	// satisfy the timeout leaf's OP_CHECKSEQUENCEVERIFY.
	signature, err := txscript.RawTxInTapscriptSignature(
		tx, sigHashes, 0, prevOutput.Value, prevOutput.PkScript,
		*harness.staticAddress.TimeoutLeaf, txscript.SigHashDefault,
		harness.clientKey,
	)
	require.NoError(t, err)

	witness, err := harness.staticAddress.GenTimeoutWitness(signature)
	require.NoError(t, err)
	witnessV2 := make(wirev2.TxWitness, len(witness))
	for idx := range witness {
		witnessV2[idx] = bytes.Clone(witness[idx])
	}
	var witnessBytes bytes.Buffer
	require.NoError(t, psbtv2.WriteTxWitness(&witnessBytes, witnessV2))
	packet.Inputs[0].FinalScriptWitness = witnessBytes.Bytes()

	serialized, err := upstream.SerializeSignature(packet)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(
		serialized, upstream.PrefixSimple,
	))

	// The generic verifier must report the precise CSV failure rather than
	// treating simple support as inconclusive.
	network := &chaincfgv2.Params{
		Bech32HRPSegwit: chaincfg.RegressionNetParams.Bech32HRPSegwit,
	}
	address, err := addressv2.NewAddressTaproot(
		harness.params.PkScript[2:], network,
	)
	require.NoError(t, err)
	valid, _, err := upstream.VerifyMessage(message, address, serialized)
	require.False(t, valid)
	require.ErrorIs(t, err, upstream.ErrInvalidSignature)
	require.ErrorContains(t, err, "ErrUnsatisfiedLockTime")
}
