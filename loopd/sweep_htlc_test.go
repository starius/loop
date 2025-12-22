package loopd

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/loop/loopdb"
	"github.com/lightninglabs/loop/looprpc"
	"github.com/lightninglabs/loop/swap"
	"github.com/lightninglabs/loop/test"
	"github.com/lightninglabs/loop/utils"
	"github.com/lightningnetwork/lnd/chainntnfs"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/stretchr/testify/require"
)

// TestSweepHtlc exercises the HTLC sweep helper using mocks.
func TestSweepHtlc(t *testing.T) {
	t.Parallel()

	lnd := test.NewMockLnd()
	store := loopdb.NewStoreMock(t)

	preimage := lntypes.Preimage{1, 2, 3, 4}
	swapHash := preimage.Hash()

	_, senderPub := test.CreateKey(0)
	_, receiverPub := test.CreateKey(1)

	var senderKey, receiverKey [33]byte
	copy(senderKey[:], senderPub.SerializeCompressed())
	copy(receiverKey[:], receiverPub.SerializeCompressed())

	htlcKeys := loopdb.HtlcKeys{
		SenderScriptKey:   senderKey,
		ReceiverScriptKey: receiverKey,
		ClientScriptKeyLocator: keychain.KeyLocator{
			Family: keychain.KeyFamily(swap.KeyFamily),
			Index:  0,
		},
	}

	swapContract := loopdb.SwapContract{
		Preimage:         preimage,
		AmountRequested:  100_000,
		HtlcKeys:         htlcKeys,
		CltvExpiry:       500,
		InitiationHeight: 123,
		ProtocolVersion:  loopdb.ProtocolVersionHtlcV2,
	}

	// Destination address isn't used in the sweep helper; set to a dummy.
	destAddr, err := btcutil.NewAddressWitnessPubKeyHash(
		make([]byte, 20), lnd.ChainParams,
	)
	require.NoError(t, err)

	loopOut := &loopdb.LoopOut{
		Loop: loopdb.Loop{
			Hash: swapHash,
		},
		Contract: &loopdb.LoopOutContract{
			SwapContract: swapContract,
			DestAddr:     destAddr,
		},
	}

	store.LoopOutSwaps[swapHash] = loopOut.Contract

	htlc, err := utils.GetHtlc(
		swapHash, &loopOut.Contract.SwapContract, lnd.ChainParams,
	)
	require.NoError(t, err)

	fundingTx := wire.NewMsgTx(2)
	fundingTx.AddTxOut(&wire.TxOut{
		Value:    int64(loopOut.Contract.AmountRequested),
		PkScript: htlc.PkScript,
	})
	fundingHash := fundingTx.TxHash()
	outpoint := wire.OutPoint{Hash: fundingHash, Index: 0}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Drain signer requests to avoid blocking.
	go func() {
		select {
		case <-lnd.SignOutputRawChannel:
		case <-ctx.Done():
		}
	}()

	respChan := make(chan *looprpc.SweepHtlcResponse, 1)
	errChan := make(chan error, 1)
	go func() {
		req := &looprpc.SweepHtlcRequest{
			Outpoint:    outpoint.String(),
			SatPerVbyte: 10,
			Publish:     false,
			HtlcAddress: htlc.Address.String(),
			DestAddress: "",
			Preimage:    nil,
		}

		resp, err := sweepHtlc(
			ctx, req, lnd.ChainParams, store,
			lnd.ChainNotifier, lnd.WalletKit, lnd.Signer,
		)
		if err != nil {
			errChan <- err
			return
		}
		respChan <- resp
	}()

	// Wait for the confirmation registration then feed it the funding tx.
	var reg *test.ConfRegistration
	select {
	case reg = <-lnd.RegisterConfChannel:
	case <-ctx.Done():
		t.Fatal("no conf registration")
	}
	require.Equal(t, htlc.PkScript, reg.PkScript)
	require.NotNil(t, reg)
	require.EqualValues(t, swapContract.InitiationHeight, reg.HeightHint)

	conf := &chainntnfs.TxConfirmation{
		Tx: fundingTx,
	}
	reg.ConfChan <- conf

	var resp *looprpc.SweepHtlcResponse
	select {
	case resp = <-respChan:
	case err := <-errChan:
		t.Fatalf("helper returned error: %v", err)
	case <-ctx.Done():
		t.Fatal("sweep helper did not finish")
	}

	require.NotEmpty(t, resp.SweepTx)

	var sweepTx wire.MsgTx
	require.NoError(t, sweepTx.Deserialize(bytes.NewReader(resp.SweepTx)))
	require.Equal(t, outpoint, sweepTx.TxIn[0].PreviousOutPoint)
	require.NotEmpty(t, sweepTx.TxIn[0].Witness)

	// Publish should not have been attempted.
	select {
	case <-lnd.TxPublishChannel:
		t.Fatal("unexpected publish")
	default:
	}
}
