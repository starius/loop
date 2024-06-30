package sweepbatcher

import (
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/lightninglabs/loop/swap"
	"github.com/lightningnetwork/lnd/input"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwallet/chainfee"
	"github.com/stretchr/testify/require"
)

// Useful constants for tests.
const (
	lowFeeRate  = chainfee.FeePerKwFloor
	highFeeRate = chainfee.SatPerKWeight(30000)

	coopInputWeight       = lntypes.WeightUnit(230)
	nonCoopInputWeight    = lntypes.WeightUnit(521)
	nonCoopPenalty        = nonCoopInputWeight - coopInputWeight
	coopNewBatchWeight    = lntypes.WeightUnit(396)
	nonCoopNewBatchWeight = coopNewBatchWeight + nonCoopPenalty
)

// testHtlcV2SuccessEstimator adds weight of non-cooperative input to estimator
// using HTLC v2.
func testHtlcV2SuccessEstimator(estimator *input.TxWeightEstimator) error {
	swapHash := lntypes.Hash{1, 1, 1}
	htlc, err := swap.NewHtlcV2(
		111, htlcKeys.SenderScriptKey, htlcKeys.ReceiverScriptKey,
		swapHash, &chaincfg.RegressionNetParams,
	)
	if err != nil {
		return err
	}
	return htlc.AddSuccessToEstimator(estimator)
}

// testHtlcV3SuccessEstimator adds weight of non-cooperative input to estimator
// using HTLC v3.
func testHtlcV3SuccessEstimator(estimator *input.TxWeightEstimator) error {
	swapHash := lntypes.Hash{1, 1, 1}
	htlc, err := swap.NewHtlcV3(
		input.MuSig2Version100RC2, 111,
		htlcKeys.SenderInternalPubKey, htlcKeys.ReceiverInternalPubKey,
		htlcKeys.SenderScriptKey, htlcKeys.ReceiverScriptKey, swapHash,
		&chaincfg.RegressionNetParams,
	)
	if err != nil {
		return err
	}
	return htlc.AddSuccessToEstimator(estimator)
}

// TestEstimateSweepFeeIncrement tests that weight and fee estimations work
// correctly for a sweep and one sweep batch.
func TestEstimateSweepFeeIncrement(t *testing.T) {
	// Useful variables reused in test cases.
	se3 := testHtlcV3SuccessEstimator
	trAddr := (*btcutil.AddressTaproot)(nil)
	p2pkhAddr := (*btcutil.AddressPubKeyHash)(nil)

	cases := []struct {
		name                   string
		sweep                  *sweep
		wantSweepFeeDetails    feeDetails
		wantNewBatchFeeDetails feeDetails
	}{
		{
			name: "regular",
			sweep: &sweep{
				minFeeRate:           lowFeeRate,
				htlcSuccessEstimator: se3,
			},
			wantSweepFeeDetails: feeDetails{
				FeeRate:       lowFeeRate,
				CoopWeight:    coopInputWeight,
				NonCoopWeight: nonCoopInputWeight,
			},
			wantNewBatchFeeDetails: feeDetails{
				FeeRate:       lowFeeRate,
				CoopWeight:    lntypes.WeightUnit(444),
				NonCoopWeight: lntypes.WeightUnit(735),
			},
		},

		{
			name: "high fee rate",
			sweep: &sweep{
				minFeeRate:           highFeeRate,
				htlcSuccessEstimator: se3,
			},
			wantSweepFeeDetails: feeDetails{
				FeeRate:       highFeeRate,
				CoopWeight:    coopInputWeight,
				NonCoopWeight: nonCoopInputWeight,
			},
			wantNewBatchFeeDetails: feeDetails{
				FeeRate:       highFeeRate,
				CoopWeight:    lntypes.WeightUnit(444),
				NonCoopWeight: lntypes.WeightUnit(735),
			},
		},

		{
			name: "isExternalAddr taproot",
			sweep: &sweep{
				minFeeRate:           lowFeeRate,
				htlcSuccessEstimator: se3,
				isExternalAddr:       true,
				destAddr:             trAddr,
			},
			wantSweepFeeDetails: feeDetails{
				FeeRate:        lowFeeRate,
				CoopWeight:     coopInputWeight,
				NonCoopWeight:  nonCoopInputWeight,
				IsExternalAddr: true,
			},
			wantNewBatchFeeDetails: feeDetails{
				FeeRate:        lowFeeRate,
				CoopWeight:     lntypes.WeightUnit(444),
				NonCoopWeight:  lntypes.WeightUnit(735),
				IsExternalAddr: true,
			},
		},

		{
			name: "isExternalAddr P2PKH",
			sweep: &sweep{
				minFeeRate:           lowFeeRate,
				htlcSuccessEstimator: se3,
				isExternalAddr:       true,
				destAddr:             p2pkhAddr,
			},
			wantSweepFeeDetails: feeDetails{
				FeeRate:        lowFeeRate,
				CoopWeight:     coopInputWeight,
				NonCoopWeight:  nonCoopInputWeight,
				IsExternalAddr: true,
			},
			wantNewBatchFeeDetails: feeDetails{
				FeeRate:        lowFeeRate,
				CoopWeight:     lntypes.WeightUnit(408),
				NonCoopWeight:  lntypes.WeightUnit(699),
				IsExternalAddr: true,
			},
		},

		{
			name: "non-coop",
			sweep: &sweep{
				minFeeRate:           lowFeeRate,
				htlcSuccessEstimator: se3,
				nonCoopHint:          true,
			},
			wantSweepFeeDetails: feeDetails{
				FeeRate:       lowFeeRate,
				CoopWeight:    coopInputWeight,
				NonCoopWeight: nonCoopInputWeight,
				NonCoopHint:   true,
			},
			wantNewBatchFeeDetails: feeDetails{
				FeeRate:       lowFeeRate,
				CoopWeight:    lntypes.WeightUnit(444),
				NonCoopWeight: lntypes.WeightUnit(735),
				NonCoopHint:   true,
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gotSweepFeeDetails, gotNewBatchFeeDetails, err :=
				estimateSweepFeeIncrement(tc.sweep)
			require.NoError(t, err)
			require.Equal(
				t, tc.wantSweepFeeDetails, gotSweepFeeDetails,
			)
			require.Equal(
				t, tc.wantNewBatchFeeDetails,
				gotNewBatchFeeDetails,
			)
		})
	}
}

// TestEstimateBatchWeight tests that weight and fee estimations work correctly
// for batches.
func TestEstimateBatchWeight(t *testing.T) {
	// Useful variables reused in test cases.
	swapHash1 := lntypes.Hash{1, 1, 1}
	swapHash2 := lntypes.Hash{2, 2, 2}
	se2 := testHtlcV2SuccessEstimator
	se3 := testHtlcV3SuccessEstimator
	trAddr := (*btcutil.AddressTaproot)(nil)

	cases := []struct {
		name                string
		batch               *batch
		wantBatchFeeDetails feeDetails
	}{
		{
			name: "one sweep regular batch",
			batch: &batch{
				id: 1,
				rbfCache: rbfCache{
					FeeRate: lowFeeRate,
				},
				sweeps: map[lntypes.Hash]sweep{
					swapHash1: {
						htlcSuccessEstimator: se3,
					},
				},
			},
			wantBatchFeeDetails: feeDetails{
				BatchId:       1,
				FeeRate:       lowFeeRate,
				CoopWeight:    lntypes.WeightUnit(444),
				NonCoopWeight: lntypes.WeightUnit(735),
			},
		},

		{
			name: "two sweeps regular batch",
			batch: &batch{
				id: 1,
				rbfCache: rbfCache{
					FeeRate: lowFeeRate,
				},
				sweeps: map[lntypes.Hash]sweep{
					swapHash1: {
						htlcSuccessEstimator: se3,
					},
					swapHash2: {
						htlcSuccessEstimator: se3,
					},
				},
			},
			wantBatchFeeDetails: feeDetails{
				BatchId:       1,
				FeeRate:       lowFeeRate,
				CoopWeight:    lntypes.WeightUnit(674),
				NonCoopWeight: lntypes.WeightUnit(1256),
			},
		},

		{
			name: "v2 and v3 sweeps",
			batch: &batch{
				id: 1,
				rbfCache: rbfCache{
					FeeRate: lowFeeRate,
				},
				sweeps: map[lntypes.Hash]sweep{
					swapHash1: {
						htlcSuccessEstimator: se2,
					},
					swapHash2: {
						htlcSuccessEstimator: se3,
					},
				},
			},
			wantBatchFeeDetails: feeDetails{
				BatchId:       1,
				FeeRate:       lowFeeRate,
				CoopWeight:    lntypes.WeightUnit(674),
				NonCoopWeight: lntypes.WeightUnit(1103),
			},
		},

		{
			name: "high fee rate",
			batch: &batch{
				id: 1,
				rbfCache: rbfCache{
					FeeRate: highFeeRate,
				},
				sweeps: map[lntypes.Hash]sweep{
					swapHash1: {
						htlcSuccessEstimator: se3,
					},
				},
			},
			wantBatchFeeDetails: feeDetails{
				BatchId:       1,
				FeeRate:       highFeeRate,
				CoopWeight:    lntypes.WeightUnit(444),
				NonCoopWeight: lntypes.WeightUnit(735),
			},
		},

		{
			name: "non-coop",
			batch: &batch{
				id: 1,
				rbfCache: rbfCache{
					FeeRate: lowFeeRate,
				},
				sweeps: map[lntypes.Hash]sweep{
					swapHash1: {
						htlcSuccessEstimator: se3,
					},
					swapHash2: {
						htlcSuccessEstimator: se3,
						nonCoopHint:          true,
					},
				},
			},
			wantBatchFeeDetails: feeDetails{
				BatchId:       1,
				FeeRate:       lowFeeRate,
				CoopWeight:    lntypes.WeightUnit(674),
				NonCoopWeight: lntypes.WeightUnit(1256),
				NonCoopHint:   true,
			},
		},

		{
			name: "isExternalAddr",
			batch: &batch{
				id: 1,
				rbfCache: rbfCache{
					FeeRate: lowFeeRate,
				},
				sweeps: map[lntypes.Hash]sweep{
					swapHash1: {
						htlcSuccessEstimator: se3,
						isExternalAddr:       true,
						destAddr:             trAddr,
					},
				},
			},
			wantBatchFeeDetails: feeDetails{
				BatchId:        1,
				FeeRate:        lowFeeRate,
				CoopWeight:     lntypes.WeightUnit(444),
				NonCoopWeight:  lntypes.WeightUnit(735),
				IsExternalAddr: true,
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gotBatchFeeDetails, err := estimateBatchWeight(tc.batch)
			require.NoError(t, err)
			require.Equal(
				t, tc.wantBatchFeeDetails, gotBatchFeeDetails,
			)
		})
	}
}

// TestSelectBatch tests greedy batch selection algorithm.
func TestSelectBatch(t *testing.T) {
	cases := []struct {
		name                 string
		batches              []feeDetails
		sweep, oneSweepBatch feeDetails
		wantBestBatchId      int32
	}{
		{
			name:    "no existing batches",
			batches: []feeDetails{},
			sweep: feeDetails{
				FeeRate:       lowFeeRate,
				CoopWeight:    coopInputWeight,
				NonCoopWeight: nonCoopInputWeight,
			},
			oneSweepBatch: feeDetails{
				FeeRate:       lowFeeRate,
				CoopWeight:    coopNewBatchWeight,
				NonCoopWeight: nonCoopNewBatchWeight,
			},
			wantBestBatchId: specialId,
		},

		{
			name: "low fee sweep, low fee existing batch",
			batches: []feeDetails{
				{
					BatchId:       1,
					FeeRate:       lowFeeRate,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
				},
			},
			sweep: feeDetails{
				FeeRate:       lowFeeRate,
				CoopWeight:    coopInputWeight,
				NonCoopWeight: nonCoopInputWeight,
			},
			oneSweepBatch: feeDetails{
				FeeRate:       lowFeeRate,
				CoopWeight:    coopNewBatchWeight,
				NonCoopWeight: nonCoopNewBatchWeight,
			},
			wantBestBatchId: 1,
		},

		{
			name: "low fee sweep, high fee existing batch",
			batches: []feeDetails{
				{
					BatchId:       1,
					FeeRate:       highFeeRate,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
				},
			},
			sweep: feeDetails{
				FeeRate:       lowFeeRate,
				CoopWeight:    coopInputWeight,
				NonCoopWeight: nonCoopInputWeight,
			},
			oneSweepBatch: feeDetails{
				FeeRate:       lowFeeRate,
				CoopWeight:    coopNewBatchWeight,
				NonCoopWeight: nonCoopNewBatchWeight,
			},
			wantBestBatchId: specialId,
		},

		{
			name: "low fee sweep, low + high fee existing batches",
			batches: []feeDetails{
				{
					BatchId:       1,
					FeeRate:       lowFeeRate,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
				},
				{
					BatchId:       2,
					FeeRate:       highFeeRate,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
				},
			},
			sweep: feeDetails{
				FeeRate:       lowFeeRate,
				CoopWeight:    coopInputWeight,
				NonCoopWeight: nonCoopInputWeight,
			},
			oneSweepBatch: feeDetails{
				FeeRate:       lowFeeRate,
				CoopWeight:    coopNewBatchWeight,
				NonCoopWeight: nonCoopNewBatchWeight,
			},
			wantBestBatchId: 1,
		},

		{
			name: "high fee sweep, low + high fee existing batches",
			batches: []feeDetails{
				{
					BatchId:       1,
					FeeRate:       lowFeeRate,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
				},
				{
					BatchId:       2,
					FeeRate:       highFeeRate,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
				},
			},
			sweep: feeDetails{
				FeeRate:       highFeeRate,
				CoopWeight:    coopInputWeight,
				NonCoopWeight: nonCoopInputWeight,
			},
			oneSweepBatch: feeDetails{
				FeeRate:       highFeeRate,
				CoopWeight:    coopNewBatchWeight,
				NonCoopWeight: nonCoopNewBatchWeight,
			},
			wantBestBatchId: 2,
		},

		{
			name: "high fee noncoop sweep",
			batches: []feeDetails{
				{
					BatchId:       1,
					FeeRate:       lowFeeRate,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
				},
				{
					BatchId:       2,
					FeeRate:       highFeeRate,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
				},
			},
			sweep: feeDetails{
				FeeRate:       highFeeRate,
				CoopWeight:    coopInputWeight,
				NonCoopWeight: nonCoopInputWeight,
				NonCoopHint:   true,
			},
			oneSweepBatch: feeDetails{
				FeeRate:       highFeeRate,
				CoopWeight:    coopNewBatchWeight,
				NonCoopWeight: nonCoopNewBatchWeight,
				NonCoopHint:   true,
			},
			wantBestBatchId: specialId,
		},

		{
			name: "high fee noncoop sweep, high batch noncoop",
			batches: []feeDetails{
				{
					BatchId:       1,
					FeeRate:       lowFeeRate,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
				},
				{
					BatchId:       2,
					FeeRate:       highFeeRate,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
					NonCoopHint:   true,
				},
			},
			sweep: feeDetails{
				FeeRate:       highFeeRate,
				CoopWeight:    coopInputWeight,
				NonCoopWeight: nonCoopInputWeight,
				NonCoopHint:   true,
			},
			oneSweepBatch: feeDetails{
				FeeRate:       highFeeRate,
				CoopWeight:    coopNewBatchWeight,
				NonCoopWeight: nonCoopNewBatchWeight,
				NonCoopHint:   true,
			},
			wantBestBatchId: 2,
		},

		{
			name: "low fee noncoop sweep",
			batches: []feeDetails{
				{
					BatchId:       1,
					FeeRate:       lowFeeRate,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
				},
				{
					BatchId:       2,
					FeeRate:       highFeeRate,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
				},
			},
			sweep: feeDetails{
				FeeRate:       lowFeeRate,
				CoopWeight:    coopInputWeight,
				NonCoopWeight: nonCoopInputWeight,
				NonCoopHint:   true,
			},
			oneSweepBatch: feeDetails{
				FeeRate:       lowFeeRate,
				CoopWeight:    coopNewBatchWeight,
				NonCoopWeight: nonCoopNewBatchWeight,
				NonCoopHint:   true,
			},
			wantBestBatchId: specialId,
		},

		{
			name: "low fee noncoop sweep, low batch noncoop",
			batches: []feeDetails{
				{
					BatchId:       1,
					FeeRate:       lowFeeRate,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
					NonCoopHint:   true,
				},
				{
					BatchId:       2,
					FeeRate:       highFeeRate,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
				},
			},
			sweep: feeDetails{
				FeeRate:       lowFeeRate,
				CoopWeight:    coopInputWeight,
				NonCoopWeight: nonCoopInputWeight,
				NonCoopHint:   true,
			},
			oneSweepBatch: feeDetails{
				FeeRate:       lowFeeRate,
				CoopWeight:    coopNewBatchWeight,
				NonCoopWeight: nonCoopNewBatchWeight,
				NonCoopHint:   true,
			},
			wantBestBatchId: 1,
		},

		{
			name: "external address sweep",
			batches: []feeDetails{
				{
					BatchId:       1,
					FeeRate:       highFeeRate,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
				},
				{
					BatchId:       2,
					FeeRate:       highFeeRate,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
				},
			},
			sweep: feeDetails{
				FeeRate:        highFeeRate,
				CoopWeight:     coopInputWeight,
				NonCoopWeight:  nonCoopInputWeight,
				IsExternalAddr: true,
			},
			oneSweepBatch: feeDetails{
				FeeRate:        highFeeRate,
				CoopWeight:     coopNewBatchWeight,
				NonCoopWeight:  nonCoopNewBatchWeight,
				IsExternalAddr: true,
			},
			wantBestBatchId: specialId,
		},

		{
			name: "external address batch",
			batches: []feeDetails{
				{
					BatchId:       1,
					FeeRate:       highFeeRate - 1,
					CoopWeight:    coopNewBatchWeight,
					NonCoopWeight: nonCoopNewBatchWeight,
				},
				{
					BatchId:        2,
					FeeRate:        highFeeRate,
					CoopWeight:     coopNewBatchWeight,
					NonCoopWeight:  nonCoopNewBatchWeight,
					IsExternalAddr: true,
				},
			},
			sweep: feeDetails{
				FeeRate:       highFeeRate,
				CoopWeight:    coopInputWeight,
				NonCoopWeight: nonCoopInputWeight,
			},
			oneSweepBatch: feeDetails{
				FeeRate:       highFeeRate,
				CoopWeight:    coopNewBatchWeight,
				NonCoopWeight: nonCoopNewBatchWeight,
			},
			wantBestBatchId: 1,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gotBestBatchId, err := selectBatch(
				tc.batches, tc.sweep, tc.oneSweepBatch,
			)
			require.NoError(t, err)
			require.Equal(t, tc.wantBestBatchId, gotBestBatchId)
		})
	}
}
