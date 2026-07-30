package loopd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	upstream "github.com/btcsuite/btcd/bip322"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/lndclient"
	"github.com/lightninglabs/loop/staticaddr/deposit"
	"github.com/lightninglabs/loop/staticaddr/script"
	"github.com/lightninglabs/loop/staticaddr/version"
	"github.com/lightninglabs/loop/swap"
	"github.com/lightningnetwork/lnd/input"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/lightningnetwork/lnd/lnwallet"
	"github.com/stretchr/testify/require"
)

// bip322AddressManagerMock returns configured parameters and sequential wallet
// snapshots.
type bip322AddressManagerMock struct {
	// params is the persisted static-address fixture.
	params *script.Parameters

	// paramsErr fails address-parameter lookup.
	paramsErr error

	// snapshots are returned in order and then repeat the final entry.
	snapshots [][]*lnwallet.Utxo

	// snapshotErr fails every wallet snapshot.
	snapshotErr error

	// listCalls records wallet snapshot requests.
	listCalls int
}

// GetStaticAddressParameters returns the configured address fixture.
func (m *bip322AddressManagerMock) GetStaticAddressParameters(
	context.Context) (*script.Parameters, error) {

	return m.params, m.paramsErr
}

// ListUnspentRaw returns the next configured wallet snapshot.
func (m *bip322AddressManagerMock) ListUnspentRaw(context.Context, int32,
	int32) (*btcutil.AddressTaproot, []*lnwallet.Utxo, error) {

	if m.snapshotErr != nil {
		return nil, nil, m.snapshotErr
	}
	if len(m.snapshots) == 0 {
		return nil, nil, nil
	}

	snapshotIndex := m.listCalls
	if snapshotIndex >= len(m.snapshots) {
		snapshotIndex = len(m.snapshots) - 1
	}
	m.listCalls++

	return nil, m.snapshots[snapshotIndex], nil
}

// bip322DepositManagerMock returns configured deposit records and tracks
// reconciliation.
type bip322DepositManagerMock struct {
	// deposits is the visible database snapshot.
	deposits []*deposit.Deposit

	// snapshots are returned in order instead of deposits when populated.
	snapshots [][]*deposit.Deposit

	// refreshErr fails reconciliation.
	refreshErr error

	// listErr fails visible-deposit lookup.
	listErr error

	// freshCalls records reconciliation attempts.
	freshCalls int

	// listCalls records visible-deposit snapshot requests.
	listCalls int
}

// EnsureDepositsFresh records and returns the configured refresh result.
func (m *bip322DepositManagerMock) EnsureDepositsFresh(
	context.Context) error {

	m.freshCalls++
	return m.refreshErr
}

// GetVisibleDeposits returns the configured database snapshot.
func (m *bip322DepositManagerMock) GetVisibleDeposits(
	context.Context) ([]*deposit.Deposit, error) {

	if len(m.snapshots) != 0 {
		snapshotIndex := m.listCalls
		if snapshotIndex >= len(m.snapshots) {
			snapshotIndex = len(m.snapshots) - 1
		}
		m.listCalls++

		return m.snapshots[snapshotIndex], m.listErr
	}

	m.listCalls++
	return m.deposits, m.listErr
}

// bip322KeyDeriverMock records the requested locator and returns a configured
// key descriptor.
type bip322KeyDeriverMock struct {
	// key is the derived client key fixture.
	key *keychain.KeyDescriptor

	// err fails derivation.
	err error

	// locator records the requested persisted locator.
	locator *keychain.KeyLocator
}

// DeriveKey records a copy of the locator before returning the configured key.
func (m *bip322KeyDeriverMock) DeriveKey(_ context.Context,
	locator *keychain.KeyLocator) (*keychain.KeyDescriptor, error) {

	locatorCopy := *locator
	m.locator = &locatorCopy
	return m.key, m.err
}

// bip322SignerMock signs requested tapscript inputs and records the complete
// detached signing context.
type bip322SignerMock struct {
	// key signs every requested input.
	key *btcec.PrivateKey

	// keyDesc is the locator and public key expected on every descriptor.
	keyDesc *keychain.KeyDescriptor

	// err fails signing before context processing.
	err error

	// signDescs records the descriptors supplied by loopd.
	signDescs []*lndclient.SignDescriptor

	// prevOutputs records the detached Taproot prevout set.
	prevOutputs []*wire.TxOut
}

// SignOutputRawKeyLocator emulates lnd Taproot script-path signing.
func (m *bip322SignerMock) SignOutputRawKeyLocator(_ context.Context,
	tx *wire.MsgTx, signDescs []*lndclient.SignDescriptor,
	prevOutputs []*wire.TxOut) ([][]byte, error) {

	if m.err != nil {
		return nil, m.err
	}
	if len(signDescs) != len(tx.TxIn) ||
		len(prevOutputs) != len(tx.TxIn) {

		return nil, errors.New("incomplete signing context")
	}
	for _, signDesc := range signDescs {
		if m.keyDesc == nil {
			continue
		}
		if signDesc.KeyDesc.KeyLocator != m.keyDesc.KeyLocator ||
			signDesc.KeyDesc.PubKey == nil ||
			!signDesc.KeyDesc.PubKey.IsEqual(m.keyDesc.PubKey) {

			return nil, errors.New("unexpected signing key descriptor")
		}
	}

	m.signDescs = signDescs
	m.prevOutputs = prevOutputs

	// Taproot sighashes commit to all inputs' amounts and scripts, so the
	// mock builds one fetcher before signing individual descriptors.
	prevOutFetcher := txscript.NewMultiPrevOutFetcher(nil)
	for idx, txIn := range tx.TxIn {
		prevOutFetcher.AddPrevOut(
			txIn.PreviousOutPoint, prevOutputs[idx],
		)
	}
	sigHashes := txscript.NewTxSigHashes(tx, prevOutFetcher)

	signatures := make([][]byte, len(signDescs))
	for idx, signDesc := range signDescs {
		leaf := txscript.NewBaseTapLeaf(signDesc.WitnessScript)
		signature, err := txscript.RawTxInTapscriptSignature(
			tx, sigHashes, signDesc.InputIndex,
			prevOutputs[idx].Value, prevOutputs[idx].PkScript, leaf,
			signDesc.HashType, m.key,
		)
		if err != nil {
			return nil, err
		}

		signatures[idx] = signature
	}

	return signatures, nil
}

// bip322ManagerHarness wires valid mocks around the loopd signing adapter.
type bip322ManagerHarness struct {
	// manager is the adapter under test.
	manager *staticAddressBip322Manager

	// address controls persisted parameters and wallet snapshots.
	address *bip322AddressManagerMock

	// deposits controls database snapshots and reconciliation.
	deposits *bip322DepositManagerMock

	// deriver controls the locator-derived key.
	deriver *bip322KeyDeriverMock

	// signer records lnd signing requests.
	signer *bip322SignerMock

	// params is the valid persisted address fixture.
	params *script.Parameters
}

// newBip322ManagerHarness creates a valid regtest manager fixture.
func newBip322ManagerHarness(t *testing.T) *bip322ManagerHarness {
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

	locator := keychain.KeyLocator{
		Family: keychain.KeyFamily(swap.StaticAddressKeyFamily),
		Index:  9,
	}
	params := &script.Parameters{
		ClientPubkey:    clientKey.PubKey(),
		ServerPubkey:    serverKey.PubKey(),
		Expiry:          expiry,
		PkScript:        pkScript,
		KeyLocator:      locator,
		ProtocolVersion: version.ProtocolVersion_V0,
	}
	addressManager := &bip322AddressManagerMock{params: params}
	depositManager := &bip322DepositManagerMock{}
	deriver := &bip322KeyDeriverMock{
		key: &keychain.KeyDescriptor{
			KeyLocator: locator,
			PubKey:     clientKey.PubKey(),
		},
	}
	signer := &bip322SignerMock{
		key: clientKey,
		keyDesc: &keychain.KeyDescriptor{
			KeyLocator: locator,
			PubKey:     clientKey.PubKey(),
		},
	}
	manager, err := newStaticAddressBip322Manager(
		staticAddressBip322Config{
			addressManager: addressManager,
			depositManager: depositManager,
			keyDeriver:     deriver,
			signer:         signer,
			chainParams:    &chaincfg.RegressionNetParams,
		},
	)
	require.NoError(t, err)

	return &bip322ManagerHarness{
		manager:  manager,
		address:  addressManager,
		deposits: depositManager,
		deriver:  deriver,
		signer:   signer,
		params:   params,
	}
}

// TestStaticAddressBip322ManagerFull checks key-locator signing without any
// deposit-manager access.
func TestStaticAddressBip322ManagerFull(t *testing.T) {
	t.Parallel()

	harness := newBip322ManagerHarness(t)
	result, err := harness.manager.SignFull(
		t.Context(), "loopd full proof",
	)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(
		result.proof.Signature, upstream.PrefixFull,
	))
	require.Equal(t, harness.params.Expiry, result.proof.ValidAtAge)
	require.Empty(t, result.proof.Coins)
	require.Equal(t, harness.params.KeyLocator, *harness.deriver.locator)
	require.Zero(t, harness.deposits.freshCalls)
	require.Len(t, harness.signer.signDescs, 1)
	require.Equal(
		t, harness.params.KeyLocator,
		harness.signer.signDescs[0].KeyDesc.KeyLocator,
	)
	require.Equal(
		t, input.TaprootScriptSpendSignMethod,
		harness.signer.signDescs[0].SignMethod,
	)
}

// TestStaticAddressBip322ManagerProofOfFunds checks reconciliation, BIP-69
// order, state metadata, and the post-signing wallet snapshot.
func TestStaticAddressBip322ManagerProofOfFunds(t *testing.T) {
	t.Parallel()

	harness := newBip322ManagerHarness(t)
	firstOutpoint := wire.OutPoint{
		Hash:  chainhash.Hash{2},
		Index: 4,
	}
	secondOutpoint := wire.OutPoint{
		Hash:  chainhash.Hash{1},
		Index: 7,
	}
	firstDeposit := &deposit.Deposit{
		OutPoint: firstOutpoint,
		Value:    20_000,
	}
	firstDeposit.SetState(deposit.Withdrawing)
	secondDeposit := &deposit.Deposit{
		OutPoint: secondOutpoint,
		Value:    30_000,
	}
	secondDeposit.SetState(deposit.LoopingIn)
	harness.deposits.deposits = []*deposit.Deposit{
		firstDeposit, secondDeposit,
	}
	snapshot := []*lnwallet.Utxo{
		{
			OutPoint:      firstOutpoint,
			Value:         firstDeposit.Value,
			Confirmations: 2,
			PkScript:      harness.params.PkScript,
		},
		{
			OutPoint:      secondOutpoint,
			Value:         secondDeposit.Value,
			Confirmations: 3,
			PkScript:      harness.params.PkScript,
		},
	}
	harness.address.snapshots = [][]*lnwallet.Utxo{snapshot, snapshot}

	result, err := harness.manager.SignProofOfFunds(
		t.Context(), "loopd funds proof",
		[]string{firstOutpoint.String(), secondOutpoint.String()}, false,
	)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(
		result.proof.Signature, upstream.PrefixProofOfFunds,
	))
	require.Equal(t, int64(50_000), result.proof.TotalAmountSat)
	require.Equal(t, secondOutpoint, result.proof.Coins[0].OutPoint)
	require.Equal(t, firstOutpoint, result.proof.Coins[1].OutPoint)
	require.Equal(
		t, deposit.LoopingIn, result.depositStates[secondOutpoint],
	)
	require.Equal(
		t, deposit.Withdrawing, result.depositStates[firstOutpoint],
	)
	require.Equal(t, 1, harness.deposits.freshCalls)
	require.Equal(t, 2, harness.address.listCalls)
	require.Len(t, harness.signer.signDescs, 3)
	require.Len(t, harness.signer.prevOutputs, 3)
}

// TestStaticAddressBip322ManagerRetriesReconciliation checks that --all
// retries a wallet coin that appears just after the first database refresh.
func TestStaticAddressBip322ManagerRetriesReconciliation(t *testing.T) {
	t.Parallel()

	harness := newBip322ManagerHarness(t)
	outpoint := wire.OutPoint{
		Hash:  chainhash.Hash{4},
		Index: 1,
	}
	knownDeposit := &deposit.Deposit{
		OutPoint: outpoint,
		Value:    20_000,
	}
	knownDeposit.SetState(deposit.Deposited)
	harness.deposits.snapshots = [][]*deposit.Deposit{
		nil, {knownDeposit},
	}
	walletSnapshot := []*lnwallet.Utxo{{
		OutPoint:      outpoint,
		Value:         knownDeposit.Value,
		Confirmations: 2,
		PkScript:      harness.params.PkScript,
	}}
	harness.address.snapshots = [][]*lnwallet.Utxo{
		walletSnapshot, walletSnapshot, walletSnapshot,
	}

	result, err := harness.manager.SignProofOfFunds(
		t.Context(), "reconciled proof", nil, true,
	)
	require.NoError(t, err)
	require.Len(t, result.proof.Coins, 1)
	require.Equal(t, outpoint, result.proof.Coins[0].OutPoint)
	require.Equal(t, 2, harness.deposits.freshCalls)
	require.Equal(t, 2, harness.deposits.listCalls)
	require.Equal(t, 3, harness.address.listCalls)
}

// TestStaticAddressBip322ManagerRejectsStaleProof checks that a coin spent
// during signing is not returned as current proof metadata.
func TestStaticAddressBip322ManagerRejectsStaleProof(t *testing.T) {
	t.Parallel()

	harness := newBip322ManagerHarness(t)
	outpoint := wire.OutPoint{
		Hash:  chainhash.Hash{3},
		Index: 1,
	}
	knownDeposit := &deposit.Deposit{
		OutPoint: outpoint,
		Value:    20_000,
	}
	knownDeposit.SetState(deposit.PublishExpirySweep)
	harness.deposits.deposits = []*deposit.Deposit{knownDeposit}
	harness.address.snapshots = [][]*lnwallet.Utxo{
		{{
			OutPoint:      outpoint,
			Value:         knownDeposit.Value,
			Confirmations: 2,
			PkScript:      harness.params.PkScript,
		}},
		nil,
	}

	_, err := harness.manager.SignProofOfFunds(
		t.Context(), "stale proof", nil, true,
	)
	require.ErrorContains(t, err, "was spent during proof construction")
	require.Equal(t, 2, harness.address.listCalls)
}

// TestStaticAddressBip322ManagerRejectsWrongDerivedKey checks that a restored
// wallet cannot silently sign with a key that differs from the address record.
func TestStaticAddressBip322ManagerRejectsWrongDerivedKey(t *testing.T) {
	t.Parallel()

	harness := newBip322ManagerHarness(t)
	otherKey, err := btcec.NewPrivateKey()
	require.NoError(t, err)
	harness.deriver.key = &keychain.KeyDescriptor{
		KeyLocator: harness.params.KeyLocator,
		PubKey:     otherKey.PubKey(),
	}

	_, err = harness.manager.SignFull(t.Context(), "wrong key")
	require.ErrorContains(t, err, "derived client key does not match")
	require.Empty(t, harness.signer.signDescs)
}

// TestStaticAddressBip322ManagerErrors covers dependency failures and malformed
// snapshot records.
func TestStaticAddressBip322ManagerErrors(t *testing.T) {
	t.Parallel()

	harness := newBip322ManagerHarness(t)
	harness.deposits.refreshErr = errors.New("refresh")
	_, err := harness.manager.SignProofOfFunds(
		t.Context(), "refresh failure", nil, true,
	)
	require.ErrorContains(t, err, "unable to refresh deposits")

	harness.deposits.refreshErr = nil
	harness.deposits.deposits = []*deposit.Deposit{nil}
	_, err = harness.manager.SignProofOfFunds(
		t.Context(), "nil deposit", nil, true,
	)
	require.ErrorContains(t, err, "nil record")

	harness.deposits.deposits = nil
	harness.address.snapshots = [][]*lnwallet.Utxo{{nil}}
	_, err = harness.manager.SignProofOfFunds(
		t.Context(), "nil UTXO", nil, true,
	)
	require.ErrorContains(t, err, "nil UTXO")

	harness.address.snapshots = nil
	harness.signer.err = errors.New("signer")
	_, err = harness.manager.SignFull(t.Context(), "signer failure")
	require.ErrorContains(t, err, "unable to sign BIP-322 packet")
}

// TestNewStaticAddressBip322ManagerValidation checks every required manager
// dependency.
func TestNewStaticAddressBip322ManagerValidation(t *testing.T) {
	t.Parallel()

	harness := newBip322ManagerHarness(t)
	valid := harness.manager.cfg

	tests := []struct {
		name   string
		mutate func(*staticAddressBip322Config)
		error  string
	}{
		{
			name: "address manager",
			mutate: func(cfg *staticAddressBip322Config) {
				cfg.addressManager = nil
			},
			error: "address manager is required",
		},
		{
			name: "deposit manager",
			mutate: func(cfg *staticAddressBip322Config) {
				cfg.depositManager = nil
			},
			error: "deposit manager is required",
		},
		{
			name: "key deriver",
			mutate: func(cfg *staticAddressBip322Config) {
				cfg.keyDeriver = nil
			},
			error: "key deriver is required",
		},
		{
			name: "signer",
			mutate: func(cfg *staticAddressBip322Config) {
				cfg.signer = nil
			},
			error: "signer is required",
		},
		{
			name: "network",
			mutate: func(cfg *staticAddressBip322Config) {
				cfg.chainParams = nil
			},
			error: "chain parameters are required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cfg := valid
			test.mutate(&cfg)
			_, err := newStaticAddressBip322Manager(cfg)
			require.ErrorContains(t, err, test.error)
		})
	}
}

// TestBip322SignerReceivesDetachedPrevouts checks that loopd passes the exact
// static-address script through lnd's separate prevout argument.
func TestBip322SignerReceivesDetachedPrevouts(t *testing.T) {
	t.Parallel()

	harness := newBip322ManagerHarness(t)
	result, err := harness.manager.SignFull(t.Context(), "detached prevouts")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, harness.signer.prevOutputs, 1)
	require.True(t, bytes.Equal(
		harness.params.PkScript,
		harness.signer.prevOutputs[0].PkScript,
	))
}
