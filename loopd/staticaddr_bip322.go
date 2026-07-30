package loopd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/lndclient"
	"github.com/lightninglabs/loop/fsm"
	"github.com/lightninglabs/loop/looprpc"
	"github.com/lightninglabs/loop/staticaddr/address"
	staticbip322 "github.com/lightninglabs/loop/staticaddr/bip322"
	"github.com/lightninglabs/loop/staticaddr/deposit"
	"github.com/lightninglabs/loop/staticaddr/script"
	"github.com/lightningnetwork/lnd/input"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/lightningnetwork/lnd/lnwallet"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// staticAddressBip322AddressManager supplies persisted address parameters and
// wallet UTXOs without exposing the concrete address manager.
type staticAddressBip322AddressManager interface {
	// GetStaticAddressParameters returns the persisted address construction.
	GetStaticAddressParameters(context.Context) (*script.Parameters, error)

	// ListUnspentRaw returns wallet UTXOs paying the static address.
	ListUnspentRaw(context.Context, int32, int32) (
		*btcutil.AddressTaproot, []*lnwallet.Utxo, error)
}

// staticAddressBip322DepositManager supplies a fresh database view of deposits.
type staticAddressBip322DepositManager interface {
	// EnsureDepositsFresh reconciles the database with the wallet.
	EnsureDepositsFresh(context.Context) error

	// GetVisibleDeposits returns every deposit relevant to the current wallet.
	GetVisibleDeposits(context.Context) ([]*deposit.Deposit, error)
}

// staticAddressBip322KeyDeriver derives the persisted timeout signing key.
type staticAddressBip322KeyDeriver interface {
	// DeriveKey derives the exact key locator supplied by the address record.
	DeriveKey(context.Context, *keychain.KeyLocator) (
		*keychain.KeyDescriptor, error)
}

// staticAddressBip322Signer signs Taproot inputs by key locator.
type staticAddressBip322Signer interface {
	// SignOutputRawKeyLocator signs all descriptors against detached prevouts.
	SignOutputRawKeyLocator(context.Context, *wire.MsgTx,
		[]*lndclient.SignDescriptor, []*wire.TxOut) ([][]byte, error)
}

// staticAddressBip322Config contains the manager's I/O dependencies.
type staticAddressBip322Config struct {
	// addressManager supplies address parameters and wallet UTXOs.
	addressManager staticAddressBip322AddressManager

	// depositManager supplies reconciled deposit records.
	depositManager staticAddressBip322DepositManager

	// keyDeriver resolves the persisted client key locator.
	keyDeriver staticAddressBip322KeyDeriver

	// signer obtains the timeout-path Schnorr signatures.
	signer staticAddressBip322Signer

	// chainParams selects the encoded Bitcoin address network.
	chainParams *chaincfg.Params
}

// staticAddressBip322Manager adapts loopd state and lnd services to the pure
// staticaddr/bip322 signing engine.
type staticAddressBip322Manager struct {
	// cfg holds the validated manager dependencies.
	cfg staticAddressBip322Config
}

// staticAddressBip322Result combines a cryptographic result with display-only
// deposit state captured before signing.
type staticAddressBip322Result struct {
	// proof is the locally checked BIP-322 result.
	proof *staticbip322.Result

	// depositStates maps each proven outpoint to its snapshot FSM state.
	depositStates map[wire.OutPoint]fsm.StateType
}

// staticAddressProofManager is the RPC-facing signing interface.
type staticAddressProofManager interface {
	// SignFull signs a message without adding real proof inputs.
	SignFull(context.Context, string) (*staticAddressBip322Result, error)

	// SignProofOfFunds signs a selected set of real deposit inputs.
	SignProofOfFunds(context.Context, string, []string, bool) (
		*staticAddressBip322Result, error)
}

// newStaticAddressBip322Manager validates dependencies and constructs the I/O
// adapter.
func newStaticAddressBip322Manager(
	cfg staticAddressBip322Config) (*staticAddressBip322Manager, error) {

	switch {
	case cfg.addressManager == nil:
		return nil, fmt.Errorf("address manager is required")

	case cfg.depositManager == nil:
		return nil, fmt.Errorf("deposit manager is required")

	case cfg.keyDeriver == nil:
		return nil, fmt.Errorf("key deriver is required")

	case cfg.signer == nil:
		return nil, fmt.Errorf("signer is required")

	case cfg.chainParams == nil:
		return nil, fmt.Errorf("chain parameters are required")
	}

	return &staticAddressBip322Manager{cfg: cfg}, nil
}

// SignFull signs a message through the static address timeout path.
func (m *staticAddressBip322Manager) SignFull(ctx context.Context,
	message string) (*staticAddressBip322Result, error) {

	params, coreParams, err := m.addressParameters(ctx)
	if err != nil {
		return nil, err
	}

	plan, err := staticbip322.PrepareFull(message, coreParams)
	if err != nil {
		return nil, err
	}
	keyDesc, err := m.deriveClientKey(ctx, params)
	if err != nil {
		return nil, err
	}
	result, err := m.sign(ctx, plan, keyDesc)
	if err != nil {
		return nil, err
	}

	return &staticAddressBip322Result{
		proof:         result,
		depositStates: make(map[wire.OutPoint]fsm.StateType),
	}, nil
}

// SignProofOfFunds signs a message and selected confirmed deposits, then
// rejects the result if any selected UTXO changed during signing.
func (m *staticAddressBip322Manager) SignProofOfFunds(ctx context.Context,
	message string, outpoints []string,
	all bool) (*staticAddressBip322Result, error) {

	params, coreParams, err := m.addressParameters(ctx)
	if err != nil {
		return nil, err
	}

	var (
		selected []staticbip322.Coin
		states   map[wire.OutPoint]fsm.StateType
	)

	// Reconcile before joining database records with wallet UTXOs. Retry
	// once if a coin appeared between reconciliation and the wallet
	// snapshot; silently omitting it would violate --all semantics.
	for attempt := range 2 {
		if err := m.cfg.depositManager.EnsureDepositsFresh(ctx); err != nil {
			return nil, fmt.Errorf("unable to refresh deposits: %w",
				err)
		}

		visible, err := m.cfg.depositManager.GetVisibleDeposits(ctx)
		if err != nil {
			return nil, fmt.Errorf("unable to list deposits: %w", err)
		}
		known, currentStates, err := knownDepositSnapshot(visible)
		if err != nil {
			return nil, err
		}

		unspent, err := m.unspentSnapshot(ctx)
		if err != nil {
			return nil, err
		}
		selected, err = staticbip322.SelectCoins(
			params.PkScript, known, unspent, staticbip322.Selection{
				All:       all,
				Outpoints: outpoints,
			},
		)
		if err == nil {
			states = currentStates
			break
		}
		if !errors.Is(err, staticbip322.ErrSnapshotChanged) ||
			attempt == 1 {

			return nil, err
		}
	}

	plan, err := staticbip322.PrepareProof(
		message, coreParams, selected,
	)
	if err != nil {
		return nil, err
	}
	keyDesc, err := m.deriveClientKey(ctx, params)
	if err != nil {
		return nil, err
	}
	result, err := m.sign(ctx, plan, keyDesc)
	if err != nil {
		return nil, err
	}

	// Capture a second wallet snapshot after signing. The proof remains
	// historical, but loopd must not knowingly return one for an already
	// spent or changed coin.
	current, err := m.unspentSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to recheck proof deposits: %w",
			err)
	}
	if err := staticbip322.ValidateStillUnspent(
		selected, current,
	); err != nil {
		return nil, err
	}

	// Attach state metadata in signed coin order without treating state as
	// part of the cryptographic proof.
	selectedStates := make(map[wire.OutPoint]fsm.StateType, len(selected))
	for _, coin := range selected {
		state, ok := states[coin.OutPoint]
		if !ok {
			return nil, fmt.Errorf("deposit %v has no state snapshot",
				coin.OutPoint)
		}
		selectedStates[coin.OutPoint] = state
	}

	return &staticAddressBip322Result{
		proof:         result,
		depositStates: selectedStates,
	}, nil
}

// addressParameters copies persisted parameters into the pure engine format.
func (m *staticAddressBip322Manager) addressParameters(
	ctx context.Context) (*script.Parameters,
	staticbip322.AddressParameters, error) {

	params, err := m.cfg.addressManager.GetStaticAddressParameters(ctx)
	if err != nil {
		return nil, staticbip322.AddressParameters{}, err
	}
	if params == nil {
		return nil, staticbip322.AddressParameters{}, fmt.Errorf(
			"static address parameters are nil",
		)
	}

	return params, staticbip322.AddressParameters{
		ClientPubKey:    params.ClientPubkey,
		ServerPubKey:    params.ServerPubkey,
		Expiry:          params.Expiry,
		PkScript:        bytes.Clone(params.PkScript),
		ProtocolVersion: params.ProtocolVersion,
		ChainParams:     m.cfg.chainParams,
	}, nil
}

// deriveClientKey re-derives the stored locator and rejects a wallet key that
// does not match the public key committed by the address.
func (m *staticAddressBip322Manager) deriveClientKey(ctx context.Context,
	params *script.Parameters) (*keychain.KeyDescriptor, error) {

	keyDesc, err := m.cfg.keyDeriver.DeriveKey(ctx, &params.KeyLocator)
	if err != nil {
		return nil, fmt.Errorf("unable to derive static address client key: "+
			"%w", err)
	}
	if keyDesc == nil || keyDesc.PubKey == nil {
		return nil, fmt.Errorf("wallet returned an empty client key")
	}
	if params.ClientPubkey == nil ||
		!keyDesc.PubKey.IsEqual(params.ClientPubkey) {

		return nil, fmt.Errorf("derived client key does not match static " +
			"address parameters")
	}

	return &keychain.KeyDescriptor{
		KeyLocator: params.KeyLocator,
		PubKey:     params.ClientPubkey,
	}, nil
}

// sign translates the engine's detached request into lnd sign descriptors and
// returns only signatures that the engine accepts.
func (m *staticAddressBip322Manager) sign(ctx context.Context,
	plan *staticbip322.Plan,
	keyDesc *keychain.KeyDescriptor) (*staticbip322.Result, error) {

	request, err := plan.SigningRequest()
	if err != nil {
		return nil, err
	}

	// Preserve every transaction, prevout, leaf, input, and sighash component
	// that the pure engine will independently recompute.
	signDescs := make([]*lndclient.SignDescriptor, len(request.Inputs))
	for idx, signingInput := range request.Inputs {
		signDescs[idx] = &lndclient.SignDescriptor{
			WitnessScript: bytes.Clone(signingInput.WitnessScript),
			KeyDesc:       *keyDesc,
			Output: wire.NewTxOut(
				signingInput.Output.Value,
				bytes.Clone(signingInput.Output.PkScript),
			),
			HashType:   txscript.SigHashDefault,
			InputIndex: signingInput.InputIndex,
			SignMethod: input.TaprootScriptSpendSignMethod,
		}
	}

	rawSignatures, err := m.cfg.signer.SignOutputRawKeyLocator(
		ctx, request.Tx, signDescs, request.PrevOutputs,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to sign BIP-322 packet: %w", err)
	}

	return plan.Finalize(rawSignatures)
}

// unspentSnapshot converts lnd's confirmed static-address UTXOs into immutable
// engine inputs.
func (m *staticAddressBip322Manager) unspentSnapshot(
	ctx context.Context) ([]staticbip322.Coin, error) {

	// A zero maximum is lnd's established sentinel for no upper
	// confirmation bound.
	_, utxos, err := m.cfg.addressManager.ListUnspentRaw(
		ctx, 1, deposit.MaxConfs,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to list static address UTXOs: %w",
			err)
	}

	coins := make([]staticbip322.Coin, len(utxos))
	for idx, utxo := range utxos {
		if utxo == nil {
			return nil, fmt.Errorf("wallet returned nil UTXO at index %d",
				idx)
		}

		coins[idx] = staticbip322.Coin{
			OutPoint:      utxo.OutPoint,
			Amount:        utxo.Value,
			Confirmations: utxo.Confirmations,
			PkScript:      bytes.Clone(utxo.PkScript),
		}
	}

	return coins, nil
}

// knownDepositSnapshot converts deposit records and captures their display
// state, rejecting nil or duplicate records.
func knownDepositSnapshot(deposits []*deposit.Deposit) (
	[]staticbip322.KnownDeposit, map[wire.OutPoint]fsm.StateType, error) {

	known := make([]staticbip322.KnownDeposit, len(deposits))
	states := make(map[wire.OutPoint]fsm.StateType, len(deposits))
	for idx, knownDeposit := range deposits {
		if knownDeposit == nil {
			return nil, nil, fmt.Errorf("deposit snapshot contains nil "+
				"record at index %d", idx)
		}

		outpoint := knownDeposit.OutPoint
		if _, ok := states[outpoint]; ok {
			return nil, nil, fmt.Errorf("duplicate deposit record for %v",
				outpoint)
		}

		known[idx] = staticbip322.KnownDeposit{
			OutPoint: outpoint,
			Amount:   knownDeposit.Value,
		}
		states[outpoint] = knownDeposit.GetState()
	}

	return known, states, nil
}

// SignStaticAddressBip322 signs a message with a static address or proves
// ownership of selected funds using its client-controlled timeout path.
func (s *swapClientServer) SignStaticAddressBip322(ctx context.Context,
	req *looprpc.StaticAddressBip322Request) (
	*looprpc.StaticAddressBip322Response, error) {

	if s.staticProofManager == nil {
		return nil, status.Error(codes.FailedPrecondition,
			"static address proof manager is unavailable")
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument,
			"BIP-322 request is required")
	}
	if !utf8.ValidString(req.Message) {
		return nil, status.Error(codes.InvalidArgument,
			"message must be valid UTF-8")
	}
	if len(req.Message) > staticbip322.MaxMessageBytes {
		return nil, status.Errorf(codes.InvalidArgument,
			"message is too large: got %d bytes, max %d",
			len(req.Message), staticbip322.MaxMessageBytes)
	}

	// Dispatch only the two variants that can satisfy the static address CSV
	// timeout leaf. BIP-322 simple uses a version-zero transaction.
	var (
		result *staticAddressBip322Result
		err    error
	)
	switch req.SignatureType {
	case looprpc.Bip322SignatureType_BIP322_SIGNATURE_TYPE_FULL:
		if req.All || len(req.Outpoints) != 0 {
			return nil, status.Error(codes.InvalidArgument,
				"full signatures do not accept deposit selection")
		}
		result, err = s.staticProofManager.SignFull(ctx, req.Message)

	case looprpc.Bip322SignatureType_BIP322_SIGNATURE_TYPE_PROOF_OF_FUNDS:
		if req.All == (len(req.Outpoints) > 0) {
			return nil, status.Error(codes.InvalidArgument,
				"select exactly one of explicit outpoints or all "+
					"deposits")
		}
		result, err = s.staticProofManager.SignProofOfFunds(
			ctx, req.Message, req.Outpoints, req.All,
		)

	default:
		return nil, status.Error(codes.InvalidArgument,
			"unsupported BIP-322 signature type")
	}
	if err != nil {
		return nil, staticAddressBip322RPCError(err, req.All)
	}

	response, err := rpcBip322Result(req.SignatureType, result)
	if err != nil {
		return nil, staticAddressBip322RPCError(err, req.All)
	}

	return response, nil
}

// staticAddressBip322RPCError translates engine and manager failures into
// stable client-facing gRPC status codes.
func staticAddressBip322RPCError(err error, all bool) error {
	switch {
	case errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):

		return status.FromContextError(err).Err()

	case errors.Is(err, staticbip322.ErrTooManyDeposits):
		if all {
			return status.Errorf(codes.InvalidArgument,
				"%v; use repeated --utxo flags to select at most "+
					"%d deposits", err,
				staticbip322.MaxProofDeposits)
		}

		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, staticbip322.ErrInvalidSelection):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, staticbip322.ErrDepositUnavailable):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, staticbip322.ErrSnapshotChanged):
		return status.Errorf(codes.Aborted, "%v; retry the request", err)

	default:
		return status.Error(codes.Internal, err.Error())
	}
}

// rpcBip322Result converts a manager result to stable RPC order and validates
// metadata that narrows when serialized.
func rpcBip322Result(signatureType looprpc.Bip322SignatureType,
	result *staticAddressBip322Result) (
	*looprpc.StaticAddressBip322Response, error) {

	if result == nil || result.proof == nil {
		return nil, fmt.Errorf("static address proof result is nil")
	}

	deposits := make(
		[]*looprpc.Bip322ProvenDeposit, 0, len(result.proof.Coins),
	)
	for _, provenCoin := range result.proof.Coins {
		if provenCoin.Confirmations < 0 ||
			provenCoin.Confirmations > math.MaxUint32 {

			return nil, fmt.Errorf("invalid confirmation count %d for %v",
				provenCoin.Confirmations, provenCoin.OutPoint)
		}
		state, ok := result.depositStates[provenCoin.OutPoint]
		if !ok {
			return nil, fmt.Errorf("deposit %v has no proof state",
				provenCoin.OutPoint)
		}

		deposits = append(deposits, &looprpc.Bip322ProvenDeposit{
			Outpoint:      provenCoin.OutPoint.String(),
			AmountSat:     int64(provenCoin.Amount),
			Confirmations: uint32(provenCoin.Confirmations),
			State:         toClientDepositState(state),
		})
	}

	return &looprpc.StaticAddressBip322Response{
		Address:        result.proof.Address,
		SignatureType:  signatureType,
		Signature:      result.proof.Signature,
		Constrained:    result.proof.Constrained,
		ValidAtTime:    result.proof.ValidAtTime,
		ValidAtAge:     result.proof.ValidAtAge,
		Deposits:       deposits,
		TotalAmountSat: result.proof.TotalAmountSat,
	}, nil
}

var (
	// Compile-time assertions keep production dependencies aligned with the
	// narrow signing interfaces above.
	_ staticAddressBip322AddressManager = (*address.Manager)(nil)
	_ staticAddressBip322DepositManager = (*deposit.Manager)(nil)
	_ staticAddressBip322KeyDeriver     = (lndclient.WalletKitClient)(nil)
	_ staticAddressBip322Signer         = (lndclient.SignerClient)(nil)
)
