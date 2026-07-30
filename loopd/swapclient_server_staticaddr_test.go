package loopd

import (
	"context"
	"errors"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btclog/v2"
	"github.com/lightninglabs/loop/fsm"
	"github.com/lightninglabs/loop/looprpc"
	"github.com/lightninglabs/loop/staticaddr/address"
	staticbip322 "github.com/lightninglabs/loop/staticaddr/bip322"
	"github.com/lightninglabs/loop/staticaddr/deposit"
	"github.com/lightninglabs/loop/staticaddr/script"
	mock_lnd "github.com/lightninglabs/loop/test"
	"github.com/lightningnetwork/lnd/lnwallet"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// staticAddrDepositStore is an in-memory deposit store for RPC tests.
type staticAddrDepositStore struct {
	// allDeposits is the complete stored fixture.
	allDeposits []*deposit.Deposit

	// byOutpoint supports individual deposit lookup.
	byOutpoint map[string]*deposit.Deposit
}

// staticAddrProofManager records RPC dispatch and returns a configured result.
type staticAddrProofManager struct {
	// result is returned by both signing variants.
	result *staticAddressBip322Result

	// message records the most recent challenge.
	message string

	// outpoints records the most recent explicit selection.
	outpoints []string

	// all records the most recent all-deposits selection.
	all bool

	// fullCalls counts full-signature dispatches.
	fullCalls int

	// proofCalls counts proof-of-funds dispatches.
	proofCalls int

	// signingError fails either signing variant.
	signingError error
}

// SignFull records a full-signature RPC dispatch.
func (m *staticAddrProofManager) SignFull(_ context.Context,
	message string) (*staticAddressBip322Result, error) {

	m.fullCalls++
	m.message = message
	return m.result, m.signingError
}

// SignProofOfFunds records a proof-of-funds RPC dispatch.
func (m *staticAddrProofManager) SignProofOfFunds(_ context.Context,
	message string, outpoints []string,
	all bool) (*staticAddressBip322Result, error) {

	m.proofCalls++
	m.message = message
	m.outpoints = outpoints
	m.all = all
	return m.result, m.signingError
}

// CreateDeposit implements deposit.Store for static address server tests.
func (s *staticAddrDepositStore) CreateDeposit(context.Context,
	*deposit.Deposit) error {

	return nil
}

// UpdateDeposit implements deposit.Store for static address server tests.
func (s *staticAddrDepositStore) UpdateDeposit(context.Context,
	*deposit.Deposit) error {

	return nil
}

// GetDeposit implements deposit.Store for static address server tests.
func (s *staticAddrDepositStore) GetDeposit(context.Context,
	deposit.ID) (*deposit.Deposit, error) {

	return nil, nil
}

// DepositForOutpoint returns the deposit for the requested outpoint.
func (s *staticAddrDepositStore) DepositForOutpoint(_ context.Context,
	outpoint string) (*deposit.Deposit, error) {

	if deposit, ok := s.byOutpoint[outpoint]; ok {
		return deposit, nil
	}

	return nil, deposit.ErrDepositNotFound
}

// AllDeposits returns all deposits seeded into the test store.
func (s *staticAddrDepositStore) AllDeposits(context.Context) (
	[]*deposit.Deposit, error) {

	return s.allDeposits, nil
}

// staticAddrTestAddressManager is the minimal address manager needed by
// deposit-manager RPC tests.
type staticAddrTestAddressManager struct{}

// GetStaticAddressParameters returns no parameters because these tests do not
// construct spends.
func (s *staticAddrTestAddressManager) GetStaticAddressParameters(
	context.Context) (*script.Parameters, error) {

	return nil, nil
}

// GetStaticAddress returns no script because these tests do not construct
// spends.
func (s *staticAddrTestAddressManager) GetStaticAddress(
	context.Context) (*script.StaticAddress, error) {

	return nil, nil
}

// ListUnspent returns an empty wallet fixture.
func (s *staticAddrTestAddressManager) ListUnspent(context.Context,
	int32, int32) ([]*lnwallet.Utxo, error) {

	return nil, nil
}

// GetTaprootAddress returns no address because these tests use stored scripts.
func (s *staticAddrTestAddressManager) GetTaprootAddress(
	*btcec.PublicKey, *btcec.PublicKey, int64) (*btcutil.AddressTaproot,
	error) {

	return nil, nil
}

// newTestDepositManager creates a deposit manager backed by seeded deposits.
func newTestDepositManager(
	deposits ...*deposit.Deposit) *deposit.Manager {

	byOutpoint := make(map[string]*deposit.Deposit, len(deposits))
	for _, deposit := range deposits {
		byOutpoint[deposit.OutPoint.String()] = deposit
	}

	return deposit.NewManager(&deposit.ManagerConfig{
		AddressManager: &staticAddrTestAddressManager{},
		Store: &staticAddrDepositStore{
			allDeposits: deposits,
			byOutpoint:  byOutpoint,
		},
	})
}

// newTestStaticAddressContext creates static address test dependencies.
func newTestStaticAddressContext(t *testing.T) (*address.Manager,
	*mock_lnd.LndMockServices) {

	t.Helper()

	mock := mock_lnd.NewMockLnd()
	_, client := mock_lnd.CreateKey(1)
	_, server := mock_lnd.CreateKey(2)

	addrStore := &mockAddressStore{
		params: []*script.Parameters{{
			ClientPubkey: client,
			ServerPubkey: server,
			Expiry:       10,
			PkScript:     []byte("pkscript"),
		}},
	}

	addrMgr, err := address.NewManager(&address.ManagerConfig{
		Store:       addrStore,
		WalletKit:   mock.WalletKit,
		ChainParams: mock.ChainParams,
	}, 1)
	require.NoError(t, err)

	return addrMgr, mock
}

// TestListStaticAddressDepositsReturnsVisibleDeposits verifies normal deposit
// listings include visible deposit records.
func TestListStaticAddressDepositsReturnsVisibleDeposits(t *testing.T) {
	t.Parallel()

	available := &deposit.Deposit{
		OutPoint: wire.OutPoint{
			Hash:  chainhash.Hash{2},
			Index: 2,
		},
	}
	available.SetState(deposit.Deposited)

	addrMgr, lnd := newTestStaticAddressContext(t)
	server := &swapClientServer{
		depositManager:       newTestDepositManager(available),
		staticAddressManager: addrMgr,
		lnd:                  &lnd.LndServices,
	}

	resp, err := server.ListStaticAddressDeposits(
		context.Background(), &looprpc.ListStaticAddressDepositsRequest{},
	)
	require.NoError(t, err)
	require.Len(t, resp.FilteredDeposits, 1)
	require.Equal(
		t, available.OutPoint.String(),
		resp.FilteredDeposits[0].Outpoint,
	)
}

// TestGetStaticAddressSummaryTotalsDeposits verifies visible deposits are
// included in static address summary totals.
func TestGetStaticAddressSummaryTotalsDeposits(t *testing.T) {
	t.Parallel()

	unconfirmed := &deposit.Deposit{
		OutPoint: wire.OutPoint{
			Hash:  chainhash.Hash{4},
			Index: 4,
		},
		Value:              btcutil.Amount(2_000),
		ConfirmationHeight: 0,
	}
	unconfirmed.SetState(deposit.Deposited)

	confirmed := &deposit.Deposit{
		OutPoint: wire.OutPoint{
			Hash:  chainhash.Hash{5},
			Index: 5,
		},
		Value:              btcutil.Amount(3_000),
		ConfirmationHeight: 123,
	}
	confirmed.SetState(deposit.Deposited)

	addrMgr, _ := newTestStaticAddressContext(t)
	server := &swapClientServer{
		depositManager: newTestDepositManager(
			unconfirmed, confirmed,
		),
		staticAddressManager: addrMgr,
	}

	resp, err := server.GetStaticAddressSummary(
		context.Background(), &looprpc.StaticAddressSummaryRequest{},
	)
	require.NoError(t, err)
	require.EqualValues(t, 2, resp.TotalNumDeposits)
	require.EqualValues(t, 2_000, resp.ValueUnconfirmedSatoshis)
	require.EqualValues(t, 3_000, resp.ValueDepositedSatoshis)
}

// TestGetLoopInQuoteRejectsUnavailableSelectedDeposit verifies manual quote
// requests fail for selected deposits that are no longer available.
func TestGetLoopInQuoteRejectsUnavailableSelectedDeposit(t *testing.T) {
	t.Parallel()
	setLogger(btclog.Disabled)

	locked := &deposit.Deposit{
		OutPoint: wire.OutPoint{
			Hash:  chainhash.Hash{6},
			Index: 6,
		},
		Value: btcutil.Amount(5_000),
	}
	locked.SetState(deposit.LoopingIn)

	addrMgr, lnd := newTestStaticAddressContext(t)
	server := &swapClientServer{
		depositManager:       newTestDepositManager(locked),
		staticAddressManager: addrMgr,
		lnd:                  &lnd.LndServices,
	}

	_, err := server.GetLoopInQuote(context.Background(), &looprpc.QuoteRequest{
		DepositOutpoints: []string{locked.OutPoint.String()},
	})
	require.ErrorContains(t, err, "is not currently available")
}

// TestSignStaticAddressBip322 checks proof dispatch and response metadata
// conversion.
func TestSignStaticAddressBip322(t *testing.T) {
	t.Parallel()

	outpoint := wire.OutPoint{
		Hash:  chainhash.Hash{8},
		Index: 2,
	}
	manager := &staticAddrProofManager{
		result: &staticAddressBip322Result{
			proof: &staticbip322.Result{
				Address:     "bcrt1ptest",
				Signature:   "pofsignature",
				Constrained: true,
				ValidAtAge:  144,
				Coins: []staticbip322.Coin{{
					OutPoint:      outpoint,
					Amount:        42_000,
					Confirmations: 3,
				}},
				TotalAmountSat: 42_000,
			},
			depositStates: map[wire.OutPoint]fsm.StateType{
				outpoint: deposit.Withdrawing,
			},
		},
	}
	server := &swapClientServer{
		staticProofManager: manager,
	}

	resp, err := server.SignStaticAddressBip322(
		context.Background(), &looprpc.StaticAddressBip322Request{
			Message:       "auditor challenge",
			SignatureType: looprpc.Bip322SignatureType_BIP322_SIGNATURE_TYPE_PROOF_OF_FUNDS,
			Outpoints:     []string{outpoint.String()},
		},
	)
	require.NoError(t, err)
	require.Equal(t, 1, manager.proofCalls)
	require.Equal(t, "auditor challenge", manager.message)
	require.Equal(t, []string{outpoint.String()}, manager.outpoints)
	require.False(t, manager.all)
	require.Equal(t, "pofsignature", resp.Signature)
	require.True(t, resp.Constrained)
	require.EqualValues(t, 144, resp.ValidAtAge)
	require.EqualValues(t, 42_000, resp.TotalAmountSat)
	require.Len(t, resp.Deposits, 1)
	require.Equal(t, looprpc.DepositState_WITHDRAWING,
		resp.Deposits[0].State)
}

// TestSignStaticAddressBip322Validation checks variant selection, message
// validation, nil requests, and unavailable manager handling.
func TestSignStaticAddressBip322Validation(t *testing.T) {
	t.Parallel()

	manager := &staticAddrProofManager{
		result: &staticAddressBip322Result{
			proof: &staticbip322.Result{},
		},
	}
	server := &swapClientServer{
		staticProofManager: manager,
	}

	_, err := server.SignStaticAddressBip322(
		context.Background(), &looprpc.StaticAddressBip322Request{
			SignatureType: looprpc.Bip322SignatureType_BIP322_SIGNATURE_TYPE_FULL,
			All:           true,
		},
	)
	require.ErrorContains(t, err, "do not accept deposit selection")
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = server.SignStaticAddressBip322(
		context.Background(), &looprpc.StaticAddressBip322Request{
			SignatureType: looprpc.Bip322SignatureType_BIP322_SIGNATURE_TYPE_PROOF_OF_FUNDS,
		},
	)
	require.ErrorContains(t, err, "select exactly one")
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = server.SignStaticAddressBip322(context.Background(), nil)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = server.SignStaticAddressBip322(
		context.Background(), &looprpc.StaticAddressBip322Request{
			Message:       string([]byte{0xff}),
			SignatureType: looprpc.Bip322SignatureType_BIP322_SIGNATURE_TYPE_FULL,
		},
	)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	unavailable := &swapClientServer{}
	_, err = unavailable.SignStaticAddressBip322(
		context.Background(), &looprpc.StaticAddressBip322Request{},
	)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// TestSignStaticAddressBip322ErrorMapping checks stable status codes for
// failures returned below the RPC handler.
func TestSignStaticAddressBip322ErrorMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		managerErr error
		request    *looprpc.StaticAddressBip322Request
		code       codes.Code
		message    string
	}{
		{
			name:       "invalid selection",
			managerErr: staticbip322.ErrInvalidSelection,
			code:       codes.InvalidArgument,
		},
		{
			name:       "deposit unavailable",
			managerErr: staticbip322.ErrDepositUnavailable,
			code:       codes.FailedPrecondition,
		},
		{
			name:       "snapshot changed",
			managerErr: staticbip322.ErrSnapshotChanged,
			code:       codes.Aborted,
			message:    "retry the request",
		},
		{
			name:       "signer failure",
			managerErr: errors.New("signer failed"),
			code:       codes.Internal,
		},
		{
			name:       "canceled",
			managerErr: context.Canceled,
			code:       codes.Canceled,
		},
		{
			name:       "too many all",
			managerErr: staticbip322.ErrTooManyDeposits,
			request: &looprpc.StaticAddressBip322Request{
				SignatureType: looprpc.Bip322SignatureType_BIP322_SIGNATURE_TYPE_PROOF_OF_FUNDS,
				All:           true,
			},
			code:    codes.InvalidArgument,
			message: "--utxo",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := test.request
			if request == nil {
				request = &looprpc.StaticAddressBip322Request{
					SignatureType: looprpc.Bip322SignatureType_BIP322_SIGNATURE_TYPE_FULL,
				}
			}
			server := &swapClientServer{
				staticProofManager: &staticAddrProofManager{
					signingError: test.managerErr,
				},
			}

			_, err := server.SignStaticAddressBip322(
				t.Context(), request,
			)
			require.Equal(t, test.code, status.Code(err))
			if test.message != "" {
				require.ErrorContains(t, err, test.message)
			}
		})
	}
}

// TestSignStaticAddressBip322ResultErrorIsInternal checks that malformed
// manager results are never returned as Unknown.
func TestSignStaticAddressBip322ResultErrorIsInternal(t *testing.T) {
	t.Parallel()

	server := &swapClientServer{
		staticProofManager: &staticAddrProofManager{},
	}
	_, err := server.SignStaticAddressBip322(
		t.Context(), &looprpc.StaticAddressBip322Request{
			SignatureType: looprpc.Bip322SignatureType_BIP322_SIGNATURE_TYPE_FULL,
		},
	)
	require.Equal(t, codes.Internal, status.Code(err))
}
