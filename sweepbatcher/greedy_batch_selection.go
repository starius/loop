package sweepbatcher

import (
	"context"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/txscript"
	sweeppkg "github.com/lightninglabs/loop/sweep"
	"github.com/lightningnetwork/lnd/input"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwallet/chainfee"
)

// greedyAddSweep selects a batch for the sweep using the greedy algorithm,
// which minimizes costs, and adds the sweep to the batch. To accomplish this,
// it first collects fee details about the sweep being added, about a potential
// new batch composed of this sweep only, and about all existing batches. Then
// it passes the data to selectBatch() function, which emulates adding the sweep
// to each batch and creating new batch for the sweep, and calculates the costs
// of each alternative. Based on the estimates of selectBatch(), this method
// adds the sweep to a batch, or creates new batch for it. If this method fails
// for whatever reason, the caller falls back to the simple algorithm (method
// handleSweep).
func (b *Batcher) greedyAddSweep(ctx context.Context, sweep *sweep) error {
	if b.customFeeRate == nil {
		return errors.New("greedy batch selection algorithm requires " +
			"setting custom fee rate provider")
	}

	// Collect weight and fee rate info about the sweep and new batch.
	sweepFeeDetails, newBatchFeeDetails, err := estimateSweepFeeIncrement(
		sweep,
	)
	if err != nil {
		return fmt.Errorf("failed to estimate tx weight for "+
			"sweep %x: %w", sweep.swapHash[:6], err)
	}

	// Collect weight and fee rate info about existing batches.
	batches := make([]feeDetails, 0, len(b.batches))
	for _, batch := range b.batches {
		newBatchFeeDetails, err := estimateBatchWeight(batch)
		if err != nil {
			return fmt.Errorf("failed to estimate tx weight for "+
				"batch %d: %w", batch.id, err)
		}
		batches = append(batches, newBatchFeeDetails)
	}

	// Run the algorithm. Get batchId of the best batch, or specialId if the
	// best option is to create new batch.
	batchId, err := selectBatch(
		batches, sweepFeeDetails, newBatchFeeDetails,
	)
	if err != nil {
		return fmt.Errorf("batch selection algorithm failed for sweep "+
			"%x: %w", sweep.swapHash[:6], err)
	}

	// If the best option is to start new batch, do it.
	if batchId == specialId {
		return b.launchNewBatch(ctx, sweep)
	}

	// Locate the batch to add the sweep to.
	batch, has := b.batches[batchId]
	if !has {
		return fmt.Errorf("batch selection algorithm returned "+
			"batch id %d which doesn't exist, for sweep %x",
			batchId, sweep.swapHash[:6])
	}

	// Add the sweep to the batch.
	accepted, err := batch.addSweep(ctx, sweep)
	if err != nil {
		return fmt.Errorf("batch selection algorithm returned "+
			"batch id %d for sweep %x, but adding failed: %w",
			batchId, sweep.swapHash[:6], err)
	}
	if !accepted {
		return fmt.Errorf("batch selection algorithm returned "+
			"batch id %d for sweep %x, but acceptance failed",
			batchId, sweep.swapHash[:6])
	}

	return nil
}

// estimateSweepFeeIncrement returns fee details for adding the sweep to a batch
// and for creating new batch with this sweep only.
func estimateSweepFeeIncrement(s *sweep) (feeDetails, feeDetails, error) {
	// Create a fake batch with this sweep.
	batch := &batch{
		rbfCache: rbfCache{
			FeeRate: s.minFeeRate,
		},
		sweeps: map[lntypes.Hash]sweep{
			s.swapHash: *s,
		},
	}

	// Estimate new batch.
	fd1, err := estimateBatchWeight(batch)
	if err != nil {
		return feeDetails{}, feeDetails{}, err
	}

	// Add the same sweep again to measure weight increments.
	swapHash2 := s.swapHash
	swapHash2[0]++
	batch.sweeps[swapHash2] = *s

	// Estimate weight of a batch with two sweeps.
	fd2, err := estimateBatchWeight(batch)
	if err != nil {
		return feeDetails{}, feeDetails{}, err
	}

	// Create feeDetails for sweep.
	sweepFeeDetails := feeDetails{
		FeeRate:        s.minFeeRate,
		NonCoopHint:    s.nonCoopHint,
		IsExternalAddr: s.isExternalAddr,

		// Calculate sweep weight as a difference.
		CoopWeight:    fd2.CoopWeight - fd1.CoopWeight,
		NonCoopWeight: fd2.NonCoopWeight - fd1.NonCoopWeight,
	}

	return sweepFeeDetails, fd1, nil
}

// estimateBatchWeight estimates batch weight and returns its fee details.
func estimateBatchWeight(batch *batch) (feeDetails, error) {
	// Make sure the batch is not empty.
	if len(batch.sweeps) == 0 {
		return feeDetails{}, errors.New("empty batch")
	}

	// Make sure fee rate is valid.
	if batch.rbfCache.FeeRate < chainfee.AbsoluteFeePerKwFloor {
		return feeDetails{}, fmt.Errorf("feeRate is too low: %v",
			batch.rbfCache.FeeRate)
	}

	// Find if the batch has at least one non-cooperative sweep.
	hasNonCoop := false
	for _, sweep := range batch.sweeps {
		if sweep.nonCoopHint {
			hasNonCoop = true
		}
	}

	// Find some sweep of the batch. It is used if there is just one sweep.
	var theSweep sweep
	for _, sweep := range batch.sweeps {
		theSweep = sweep
		break
	}

	// Find sweep destination address (type) for weight estimations.
	var destAddr btcutil.Address
	if theSweep.isExternalAddr {
		if theSweep.destAddr == nil {
			return feeDetails{}, errors.New("isExternalAddr=true, " +
				"but destAddr is nil")
		}
		destAddr = theSweep.destAddr
	} else {
		// Assume it is taproot by default.
		destAddr = (*btcutil.AddressTaproot)(nil)
	}

	// Make two estimators: for coop and non-coop cases.
	var coopWeight, nonCoopWeight input.TxWeightEstimator

	// Add output weight to the estimator.
	err := sweeppkg.AddOutputEstimate(&coopWeight, destAddr)
	if err != nil {
		return feeDetails{}, fmt.Errorf("sweep.AddOutputEstimate: %w",
			err)
	}
	err = sweeppkg.AddOutputEstimate(&nonCoopWeight, destAddr)
	if err != nil {
		return feeDetails{}, fmt.Errorf("sweep.AddOutputEstimate: %w",
			err)
	}

	// Add inputs.
	for _, sweep := range batch.sweeps {
		coopWeight.AddTaprootKeySpendInput(txscript.SigHashDefault)

		err = sweep.htlcSuccessEstimator(&nonCoopWeight)
		if err != nil {
			return feeDetails{}, fmt.Errorf("htlcSuccessEstimator "+
				"failed: %w", err)
		}
	}

	return feeDetails{
		BatchId:        batch.id,
		FeeRate:        batch.rbfCache.FeeRate,
		CoopWeight:     coopWeight.Weight(),
		NonCoopWeight:  nonCoopWeight.Weight(),
		NonCoopHint:    hasNonCoop,
		IsExternalAddr: theSweep.isExternalAddr,
	}, nil
}

// specialId is the value that indicates a new batch. It is returned by
// selectBatch if the most cost-efficient action is new batch creation.
const specialId = -1

// feeDetails is either a batch or a sweep and it holds data important for
// selection of a batch to add the sweep to (or new batch creation).
type feeDetails struct {
	BatchId        int32
	FeeRate        chainfee.SatPerKWeight
	CoopWeight     lntypes.WeightUnit
	NonCoopWeight  lntypes.WeightUnit
	NonCoopHint    bool
	IsExternalAddr bool
}

// fee returns fee of onchain transaction representing this instance.
func (e feeDetails) fee() btcutil.Amount {
	var weight lntypes.WeightUnit
	if e.NonCoopHint {
		weight = e.NonCoopWeight
	} else {
		weight = e.CoopWeight
	}

	return e.FeeRate.FeeForWeight(weight)
}

// combine returns new feeDetails, combining properties.
func (e1 feeDetails) combine(e2 feeDetails) feeDetails {
	// The fee rate is max of two fee rates.
	feeRate := e1.FeeRate
	if feeRate < e2.FeeRate {
		feeRate = e2.FeeRate
	}

	return feeDetails{
		FeeRate:        feeRate,
		CoopWeight:     e1.CoopWeight + e2.CoopWeight,
		NonCoopWeight:  e1.NonCoopWeight + e2.NonCoopWeight,
		NonCoopHint:    e1.NonCoopHint || e2.NonCoopHint,
		IsExternalAddr: e1.IsExternalAddr || e2.IsExternalAddr,
	}
}

// selectBatch returns index of the batch adding to which minimizes costs.
// For each batch its fee rate and two weight are provided: weight in case of
// cooperative spending and weight in case non-cooperative spending (using
// preimages instead of taproot key spend). Also a hint is provided to signal
// if the batch has to use non-cooperative spending path. The same data is also
// provided to the sweep for which we are selecting a batch to add. In case of
// the sweep weights are weight deltas resulted from adding the sweep. Finally,
// the same data is provided for new batch having this sweep only. The algorithm
// compares costs of adding the sweep to each existing batch, and costs of new
// batch creation for this sweep and returns BatchId of the winning batch. If
// the best option is to create a new batch, it returns specialId. Each fee
// details has also IsExternalAddr flag. There is a rule that sweeps having flag
// IsExternalAddr must go in individual batches. Cooperative spending is only
// available if all the sweeps support cooperative spending path.
func selectBatch(batches []feeDetails, sweep, oneSweepBatch feeDetails) (int32,
	error) {

	// Track the best batch to add a sweep to. The default case is new batch
	// creation with this sweep only in it. The cost is its full fee.
	bestBatchId := int32(specialId)
	bestCost := oneSweepBatch.fee()

	// Try to add the sweep to every batch, calculate the costs and
	// find the batch adding to which results in minimum costs.
	for _, batch := range batches {
		// If either the batch or the sweep has IsExternalAddr flag,
		// the sweep can't be added to the batch, so skip.
		if batch.IsExternalAddr || sweep.IsExternalAddr {
			continue
		}

		// Add the sweep to the batch virtually.
		combinedBatch := batch.combine(sweep)

		// The cost is fee increase.
		cost := combinedBatch.fee() - batch.fee()

		// The cost must be positive, because we added a sweep.
		if cost <= 0 {
			return 0, fmt.Errorf("got non-positive cost of adding "+
				"sweep to batch %d: %d", batch.BatchId, cost)
		}

		// Track the best batch, according to the costs.
		if bestCost > cost {
			bestBatchId = batch.BatchId
			bestCost = cost
		}
	}

	return bestBatchId, nil
}
