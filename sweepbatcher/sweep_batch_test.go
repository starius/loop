package sweepbatcher

import (
	"fmt"
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/loop/loopdb"
	"github.com/lightninglabs/loop/utils"
	"github.com/lightningnetwork/lnd/input"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwallet/chainfee"
	"github.com/stretchr/testify/require"
)

// TestConstructUnsignedTx tests that function constructUnsignedTx makes
// unsigned transactions correctly.
func TestConstructUnsignedTx(t *testing.T) {
	// Prepare some data used in test cases.
	op1 := wire.OutPoint{
		Hash:  chainhash.Hash{1, 1, 1},
		Index: 1,
	}
	op2 := wire.OutPoint{
		Hash:  chainhash.Hash{2, 2, 2},
		Index: 2,
	}

	batchPkScript, err := txscript.PayToAddrScript(destAddr)
	require.NoError(t, err)

	p2trAddr := "bcrt1pa38tp2hgjevqv3jcsxeu7v72n0s5a3ck8q2u8r" +
		"k6mm67dv7uk26qq8je7e"
	p2trAddress, err := btcutil.DecodeAddress(p2trAddr, nil)
	require.NoError(t, err)
	p2trPkScript, err := txscript.PayToAddrScript(p2trAddress)
	require.NoError(t, err)

	serializedPubKey := []byte{
		0x02, 0x19, 0x2d, 0x74, 0xd0, 0xcb, 0x94, 0x34, 0x4c, 0x95,
		0x69, 0xc2, 0xe7, 0x79, 0x01, 0x57, 0x3d, 0x8d, 0x79, 0x03,
		0xc3, 0xeb, 0xec, 0x3a, 0x95, 0x77, 0x24, 0x89, 0x5d, 0xca,
		0x52, 0xc6, 0xb4}
	p2pkAddress, err := btcutil.NewAddressPubKey(
		serializedPubKey, &chaincfg.RegressionNetParams,
	)
	require.NoError(t, err)

	swapHash := lntypes.Hash{1, 1, 1}

	swapContract := &loopdb.SwapContract{
		CltvExpiry:      222,
		AmountRequested: 2_000_000,
		ProtocolVersion: loopdb.ProtocolVersionMuSig2,
		HtlcKeys:        htlcKeys,
	}

	htlc, err := utils.GetHtlc(
		swapHash, swapContract, &chaincfg.RegressionNetParams,
	)
	require.NoError(t, err)
	estimator := htlc.AddSuccessToEstimator

	brokenEstimator := func(*input.TxWeightEstimator) error {
		return fmt.Errorf("weight estimator test failure")
	}

	cases := []struct {
		name             string
		sweeps           []sweep
		address          btcutil.Address
		currentHeight    int32
		feeRate          chainfee.SatPerKWeight
		wantErr          string
		wantTx           *wire.MsgTx
		wantWeight       lntypes.WeightUnit
		wantFeeForWeight btcutil.Amount
		wantFee          btcutil.Amount
	}{
		{
			name:    "no sweeps error",
			wantErr: "no sweeps in batch",
		},

		{
			name: "two coop sweeps",
			sweeps: []sweep{
				{
					outpoint: op1,
					value:    1_000_000,
				},
				{
					outpoint: op2,
					value:    2_000_000,
				},
			},
			address:       destAddr,
			currentHeight: 800_000,
			feeRate:       1000,
			wantTx: &wire.MsgTx{
				Version:  2,
				LockTime: 800_000,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
					},
					{
						PreviousOutPoint: op2,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
			},
			wantWeight:       626,
			wantFeeForWeight: 626,
			wantFee:          626,
		},

		{
			name: "p2tr destination address",
			sweeps: []sweep{
				{
					outpoint: op1,
					value:    1_000_000,
				},
				{
					outpoint: op2,
					value:    2_000_000,
				},
			},
			address:       p2trAddress,
			currentHeight: 800_000,
			feeRate:       1000,
			wantTx: &wire.MsgTx{
				Version:  2,
				LockTime: 800_000,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
					},
					{
						PreviousOutPoint: op2,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999326,
						PkScript: p2trPkScript,
					},
				},
			},
			wantWeight:       674,
			wantFeeForWeight: 674,
			wantFee:          674,
		},

		{
			name: "unknown kind of address",
			sweeps: []sweep{
				{
					outpoint: op1,
					value:    1_000_000,
				},
				{
					outpoint: op2,
					value:    2_000_000,
				},
			},
			address: nil,
			wantErr: "unsupported address type",
		},

		{
			name: "pay-to-pubkey address",
			sweeps: []sweep{
				{
					outpoint: op1,
					value:    1_000_000,
				},
				{
					outpoint: op2,
					value:    2_000_000,
				},
			},
			address: p2pkAddress,
			wantErr: "unknown address type",
		},

		{
			name: "fee more than 20% clamped",
			sweeps: []sweep{
				{
					outpoint: op1,
					value:    1_000_000,
				},
				{
					outpoint: op2,
					value:    2_000_000,
				},
			},
			address:       destAddr,
			currentHeight: 800_000,
			feeRate:       1_000_000,
			wantTx: &wire.MsgTx{
				Version:  2,
				LockTime: 800_000,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
					},
					{
						PreviousOutPoint: op2,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2400000,
						PkScript: batchPkScript,
					},
				},
			},
			wantWeight:       626,
			wantFeeForWeight: 626_000,
			wantFee:          600_000,
		},

		{
			name: "coop and noncoop",
			sweeps: []sweep{
				{
					outpoint: op1,
					value:    1_000_000,
				},
				{
					outpoint:             op2,
					value:                2_000_000,
					nonCoopHint:          true,
					htlc:                 *htlc,
					htlcSuccessEstimator: estimator,
				},
			},
			address:       destAddr,
			currentHeight: 800_000,
			feeRate:       1000,
			wantTx: &wire.MsgTx{
				Version:  2,
				LockTime: 800_000,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         1,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999211,
						PkScript: batchPkScript,
					},
				},
			},
			wantWeight:       789,
			wantFeeForWeight: 789,
			wantFee:          789,
		},

		{
			name: "weight estimator fails",
			sweeps: []sweep{
				{
					outpoint: op1,
					value:    1_000_000,
				},
				{
					outpoint:             op2,
					value:                2_000_000,
					nonCoopHint:          true,
					htlc:                 *htlc,
					htlcSuccessEstimator: brokenEstimator,
				},
			},
			address:       destAddr,
			currentHeight: 800_000,
			feeRate:       1000,
			wantErr: "sweep.htlcSuccessEstimator failed: " +
				"weight estimator test failure",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tx, weight, feeForW, fee, err := constructUnsignedTx(
				tc.sweeps, tc.address, tc.currentHeight,
				tc.feeRate,
			)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.wantTx, tx)
				require.Equal(t, tc.wantWeight, weight)
				require.Equal(t, tc.wantFeeForWeight, feeForW)
				require.Equal(t, tc.wantFee, fee)
			}
		})
	}
}

// TestCheckSignedTx tests that function checkSignedTx checks all the criteria
// of SignMuSig2 correctly.
func TestCheckSignedTx(t *testing.T) {
	// Prepare some data used in test cases.
	op1 := wire.OutPoint{
		Hash:  chainhash.Hash{1, 1, 1},
		Index: 1,
	}
	op2 := wire.OutPoint{
		Hash:  chainhash.Hash{2, 2, 2},
		Index: 2,
	}

	batchPkScript, err := txscript.PayToAddrScript(destAddr)
	require.NoError(t, err)

	cases := []struct {
		name        string
		unsignedTx  *wire.MsgTx
		signedTx    *wire.MsgTx
		inputAmt    btcutil.Amount
		minRelayFee chainfee.SatPerKWeight
		wantErr     string
	}{
		{
			name: "success",
			unsignedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         2,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 800_000,
			},
			signedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op2,
						Sequence:         2,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
					{
						PreviousOutPoint: op1,
						Sequence:         1,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 799_999,
			},
			inputAmt:    3_000_000,
			minRelayFee: 253,
			wantErr:     "",
		},

		{
			name: "bad locktime",
			unsignedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         2,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 800_000,
			},
			signedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op2,
						Sequence:         2,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
					{
						PreviousOutPoint: op1,
						Sequence:         1,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 800_001,
			},
			inputAmt:    3_000_000,
			minRelayFee: 253,
			wantErr:     "locktime",
		},

		{
			name: "bad version",
			unsignedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         2,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 800_000,
			},
			signedTx: &wire.MsgTx{
				Version: 3,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op2,
						Sequence:         2,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
					{
						PreviousOutPoint: op1,
						Sequence:         1,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 799_999,
			},
			inputAmt:    3_000_000,
			minRelayFee: 253,
			wantErr:     "version",
		},

		{
			name: "missing input",
			unsignedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         2,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 800_000,
			},
			signedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op2,
						Sequence:         2,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 799_999,
			},
			inputAmt:    3_000_000,
			minRelayFee: 253,
			wantErr:     "is missing in signed tx",
		},

		{
			name: "extra input",
			unsignedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 800_000,
			},
			signedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op2,
						Sequence:         2,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
					{
						PreviousOutPoint: op1,
						Sequence:         1,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 799_999,
			},
			inputAmt:    3_000_000,
			minRelayFee: 253,
			wantErr:     "is new in signed tx",
		},

		{
			name: "mismatch of sequence numbers",
			unsignedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         2,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 800_000,
			},
			signedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op2,
						Sequence:         2,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
					{
						PreviousOutPoint: op1,
						Sequence:         3,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 799_999,
			},
			inputAmt:    3_000_000,
			minRelayFee: 253,
			wantErr:     "sequence mismatch",
		},

		{
			name: "extra output in unsignedTx",
			unsignedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         2,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 800_000,
			},
			signedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op2,
						Sequence:         2,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
					{
						PreviousOutPoint: op1,
						Sequence:         1,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 799_999,
			},
			inputAmt:    3_000_000,
			minRelayFee: 253,
			wantErr:     "unsigned tx has 2 outputs, want 1",
		},

		{
			name: "extra output in signedTx",
			unsignedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         2,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 800_000,
			},
			signedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op2,
						Sequence:         2,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
					{
						PreviousOutPoint: op1,
						Sequence:         1,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 799_999,
			},
			inputAmt:    3_000_000,
			minRelayFee: 253,
			wantErr:     "the signed tx has 2 outputs, want 1",
		},

		{
			name: "mismatch of output pk_script",
			unsignedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         2,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 800_000,
			},
			signedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op2,
						Sequence:         2,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
					{
						PreviousOutPoint: op1,
						Sequence:         1,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript[1:],
					},
				},
				LockTime: 799_999,
			},
			inputAmt:    3_000_000,
			minRelayFee: 253,
			wantErr:     "mismatch of output pkScript",
		},

		{
			name: "too low feerate in signedTx",
			unsignedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         2,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 800_000,
			},
			signedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op2,
						Sequence:         2,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
					{
						PreviousOutPoint: op1,
						Sequence:         1,
						Witness: wire.TxWitness{
							[]byte("test"),
						},
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
				LockTime: 799_999,
			},
			inputAmt:    3_000_000,
			minRelayFee: 250_000,
			wantErr:     "is lower than minRelayFee",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := checkSignedTx(
				tc.unsignedTx, tc.signedTx, tc.inputAmt,
				tc.minRelayFee,
			)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestIsCPFPNeeded tests that function isCPFPNeeded works correctly, satisfying
// feeRateThresholdPPM.
func TestIsCPFPNeeded(t *testing.T) {
	// Prepare some data used in test cases.
	op1 := wire.OutPoint{
		Hash:  chainhash.Hash{1, 1, 1},
		Index: 1,
	}
	op2 := wire.OutPoint{
		Hash:  chainhash.Hash{2, 2, 2},
		Index: 2,
	}

	batchPkScript, err := txscript.PayToAddrScript(destAddr)
	require.NoError(t, err)

	witness := wire.TxWitness{
		make([]byte, 64),
	}

	cases := []struct {
		name          string
		signedTx      *wire.MsgTx
		inputAmt      btcutil.Amount
		feeRate       chainfee.SatPerKWeight
		wantErr       string
		wantNeedsCPFP bool
	}{
		{
			name: "fee rate matches exacly",
			signedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
						Witness:          witness,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         2,
						Witness:          witness,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
			},
			inputAmt:      3_000_000,
			feeRate:       1000,
			wantErr:       "",
			wantNeedsCPFP: false,
		},
		{
			name: "fee rate higher than needed",
			signedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
						Witness:          witness,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         2,
						Witness:          witness,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
			},
			inputAmt:      3_000_000,
			feeRate:       900,
			wantErr:       "",
			wantNeedsCPFP: false,
		},
		{
			name: "fee rate slightly lower than needed",
			signedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
						Witness:          witness,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         2,
						Witness:          witness,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
			},
			inputAmt:      3_000_000,
			feeRate:       1020,
			wantErr:       "",
			wantNeedsCPFP: false,
		},
		{
			name: "fee rate significantly lower than needed",
			signedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
						Witness:          witness,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         2,
						Witness:          witness,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
			},
			inputAmt:      3_000_000,
			feeRate:       1100,
			wantErr:       "",
			wantNeedsCPFP: true,
		},
		{
			name: "error: tx has negative fee",
			signedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
						Witness:          witness,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         2,
						Witness:          witness,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    3_001_000,
						PkScript: batchPkScript,
					},
				},
			},
			inputAmt: 3_000_000,
			feeRate:  1000,
			wantErr:  "negative fee",
		},
		{
			name: "error: tx has multiple outputs",
			signedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
						Witness:          witness,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         2,
						Witness:          witness,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    1_000_000,
						PkScript: batchPkScript,
					},
					{
						Value:    2_000_000,
						PkScript: batchPkScript,
					},
				},
			},
			inputAmt: 3_000_000,
			feeRate:  1000,
			wantErr:  "must have one output",
		},
		{
			name: "error: unsigned tx",
			signedTx: &wire.MsgTx{
				Version: 2,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op1,
						Sequence:         1,
					},
					{
						PreviousOutPoint: op2,
						Sequence:         2,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2999374,
						PkScript: batchPkScript,
					},
				},
			},
			inputAmt: 3_000_000,
			feeRate:  1000,
			wantErr:  "the tx must be signed",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			needsCPFP, err := isCPFPNeeded(
				tc.signedTx, tc.inputAmt, tc.feeRate,
			)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.wantNeedsCPFP, needsCPFP)
			}
		})
	}
}

// TestMakeUnsignedCPFP tests that function makeUnsignedCPFP works correctly,
// satisfying maxChildFeeSharePPM and making sure that child fee rate is higher
// than effective fee rate and of minRelayFee.
func TestMakeUnsignedCPFP(t *testing.T) {
	// Prepare some data used in test cases.
	batchPkScript, err := txscript.PayToAddrScript(destAddr)
	require.NoError(t, err)

	p2trAddr := "bcrt1pa38tp2hgjevqv3jcsxeu7v72n0s5a3ck8q2u8r" +
		"k6mm67dv7uk26qq8je7e"
	p2trAddress, err := btcutil.DecodeAddress(p2trAddr, nil)
	require.NoError(t, err)
	p2trPkScript, err := txscript.PayToAddrScript(p2trAddress)
	require.NoError(t, err)

	serializedPubKey := []byte{
		0x02, 0x19, 0x2d, 0x74, 0xd0, 0xcb, 0x94, 0x34, 0x4c, 0x95,
		0x69, 0xc2, 0xe7, 0x79, 0x01, 0x57, 0x3d, 0x8d, 0x79, 0x03,
		0xc3, 0xeb, 0xec, 0x3a, 0x95, 0x77, 0x24, 0x89, 0x5d, 0xca,
		0x52, 0xc6, 0xb4}
	p2pkAddress, err := btcutil.NewAddressPubKey(
		serializedPubKey, &chaincfg.RegressionNetParams,
	)
	require.NoError(t, err)

	batchTxid := chainhash.Hash{5, 5, 5}

	op := wire.OutPoint{
		Hash:  batchTxid,
		Index: 0,
	}

	cases := []struct {
		name              string
		parentTxid        chainhash.Hash
		parentOutput      btcutil.Amount
		parentWeight      lntypes.WeightUnit
		parentFee         btcutil.Amount
		minRelayFee       chainfee.SatPerKWeight
		effectiveFeeRate  chainfee.SatPerKWeight
		address           btcutil.Address
		currentHeight     int32
		wantErr           string
		wantUnsignedChild *wire.MsgTx
		wantChildFeeRate  chainfee.SatPerKWeight
	}{
		{
			name:             "normal child creation",
			parentTxid:       batchTxid,
			parentOutput:     2999374,
			parentWeight:     626,
			parentFee:        626,
			minRelayFee:      253,
			effectiveFeeRate: 2000,
			address:          p2trAddress,
			currentHeight:    800_000,
			wantUnsignedChild: &wire.MsgTx{
				Version:  2,
				LockTime: 800_000,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2997860,
						PkScript: p2trPkScript,
					},
				},
			},
			wantChildFeeRate: 3410,
		},
		{
			name:             "p2wpkh address",
			parentTxid:       batchTxid,
			parentOutput:     2999374,
			parentWeight:     626,
			parentFee:        626,
			minRelayFee:      253,
			effectiveFeeRate: 2000,
			address:          destAddr,
			currentHeight:    800_000,
			wantUnsignedChild: &wire.MsgTx{
				Version:  2,
				LockTime: 800_000,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2997870,
						PkScript: batchPkScript,
					},
				},
			},
			wantChildFeeRate: 3426,
		},
		{
			name:             "error: p2pk address",
			parentTxid:       batchTxid,
			parentOutput:     2999374,
			parentWeight:     626,
			parentFee:        626,
			minRelayFee:      253,
			effectiveFeeRate: 2000,
			address:          p2pkAddress,
			currentHeight:    800_000,
			wantErr:          "unknown address type",
		},
		{
			name:             "effective feerate as in parent",
			parentTxid:       batchTxid,
			parentOutput:     2999374,
			parentWeight:     626,
			parentFee:        626,
			minRelayFee:      253,
			effectiveFeeRate: 1000,
			address:          p2trAddress,
			currentHeight:    800_000,
			wantUnsignedChild: &wire.MsgTx{
				Version:  2,
				LockTime: 800_000,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2998930,
						PkScript: p2trPkScript,
					},
				},
			},
			wantChildFeeRate: 1000,
		},
		{
			name:             "effective feerate below parent",
			parentTxid:       batchTxid,
			parentOutput:     2999374,
			parentWeight:     626,
			parentFee:        626,
			minRelayFee:      253,
			effectiveFeeRate: 500,
			address:          p2trAddress,
			currentHeight:    800_000,
			wantErr:          "lower than effective fee rate",
		},
		{
			name:             "high minRelayFee",
			parentTxid:       batchTxid,
			parentOutput:     2999374,
			parentWeight:     626,
			parentFee:        626,
			minRelayFee:      10_000,
			effectiveFeeRate: 2000,
			address:          p2trAddress,
			currentHeight:    800_000,
			wantUnsignedChild: &wire.MsgTx{
				Version:  2,
				LockTime: 800_000,
				TxIn: []*wire.TxIn{
					{
						PreviousOutPoint: op,
					},
				},
				TxOut: []*wire.TxOut{
					{
						Value:    2994934,
						PkScript: p2trPkScript,
					},
				},
			},
			wantChildFeeRate: 10_000,
		},
		{
			name:             "child fee too high",
			parentTxid:       batchTxid,
			parentOutput:     2999374,
			parentWeight:     626,
			parentFee:        626,
			minRelayFee:      253,
			effectiveFeeRate: 750_000,
			address:          p2trAddress,
			currentHeight:    800_000,
			wantErr:          "is higher than 20% of total funds",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			childTx, childFeeRate, err := makeUnsignedCPFP(
				tc.parentTxid, tc.parentOutput, tc.parentWeight,
				tc.parentFee, tc.minRelayFee,
				tc.effectiveFeeRate, tc.address,
				tc.currentHeight,
			)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.wantUnsignedChild, childTx)
				require.Equal(
					t, tc.wantChildFeeRate, childFeeRate,
				)
			}
		})
	}
}
