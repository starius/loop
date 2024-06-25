package sweepbatcher

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/lndclient"
	"github.com/lightninglabs/loop/loopdb"
	"github.com/lightninglabs/loop/swap"
	"github.com/lightninglabs/loop/utils"
	"github.com/lightningnetwork/lnd/input"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwallet/chainfee"
)

const (
	// defaultMaxTimeoutDistance is the default maximum timeout distance
	// of sweeps that can appear in the same batch.
	defaultMaxTimeoutDistance = 288

	// batchOpen is the string representation of the state of a batch that
	// is open.
	batchOpen = "open"

	// batchClosed is the string representation of the state of a batch
	// that is closed.
	batchClosed = "closed"

	// batchConfirmed is the string representation of the state of a batch
	// that is confirmed.
	batchConfirmed = "confirmed"

	// defaultMainnetPublishDelay is the default publish delay that is used
	// for mainnet.
	defaultMainnetPublishDelay = 5 * time.Second

	// defaultTestnetPublishDelay is the default publish delay that is used
	// for testnet.
	defaultPublishDelay = 500 * time.Millisecond
)

type BatcherStore interface {
	// FetchUnconfirmedSweepBatches fetches all the batches from the
	// database that are not in a confirmed state.
	FetchUnconfirmedSweepBatches(ctx context.Context) ([]*dbBatch, error)

	// InsertSweepBatch inserts a batch into the database, returning the id
	// of the inserted batch.
	InsertSweepBatch(ctx context.Context, batch *dbBatch) (int32, error)

	// DropBatch drops a batch from the database. This should only be used
	// when a batch is empty.
	DropBatch(ctx context.Context, id int32) error

	// UpdateSweepBatch updates a batch in the database.
	UpdateSweepBatch(ctx context.Context, batch *dbBatch) error

	// ConfirmBatch confirms a batch by setting its state to confirmed.
	ConfirmBatch(ctx context.Context, id int32) error

	// FetchBatchSweeps fetches all the sweeps that belong to a batch.
	FetchBatchSweeps(ctx context.Context, id int32) ([]*dbSweep, error)

	// UpsertSweep inserts a sweep into the database, or updates an existing
	// sweep if it already exists.
	UpsertSweep(ctx context.Context, sweep *dbSweep) error

	// GetSweepStatus returns the completed status of the sweep.
	GetSweepStatus(ctx context.Context, swapHash lntypes.Hash) (bool, error)

	// GetParentBatch returns the parent batch of a (completed) sweep.
	GetParentBatch(ctx context.Context, swapHash lntypes.Hash) (*dbBatch,
		error)

	// TotalSweptAmount returns the total amount swept by a (confirmed)
	// batch.
	TotalSweptAmount(ctx context.Context, id int32) (btcutil.Amount, error)
}

// SweepInfo stores any data related to sweeping a specific outpoint.
type SweepInfo struct {
	// ConfTarget is the confirmation target of the sweep.
	ConfTarget int32

	// Timeout is the timeout of the swap that the sweep belongs to.
	Timeout int32

	// InitiationHeight is the height at which the swap was initiated.
	InitiationHeight int32

	// HTLC is the HTLC that is being swept.
	HTLC swap.Htlc

	// Preimage is the preimage of the HTLC that is being swept.
	Preimage lntypes.Preimage

	// SwapInvoicePaymentAddr is the payment address of the swap invoice.
	SwapInvoicePaymentAddr [32]byte

	// HTLCKeys is the set of keys used to sign the HTLC.
	HTLCKeys loopdb.HtlcKeys

	// HTLCSuccessEstimator is a function that estimates the weight of the
	// HTLC success script.
	HTLCSuccessEstimator func(*input.TxWeightEstimator) error

	// ProtocolVersion is the protocol version of the swap that the sweep
	// belongs to.
	ProtocolVersion loopdb.ProtocolVersion

	// IsExternalAddr is true if the sweep spends to a non-wallet address.
	IsExternalAddr bool

	// DestAddr is the destination address of the sweep.
	DestAddr btcutil.Address

	// MinFeeRate is minimum fee rate that must be used by a batch of
	// the sweep. If it is specified, confTarget is ignored.
	MinFeeRate chainfee.SatPerKWeight
}

// SweepFetcher is used to get details of a sweep.
type SweepFetcher interface {
	// FetchSweep returns details of the sweep with the given hash.
	FetchSweep(ctx context.Context, hash lntypes.Hash) (*SweepInfo, error)
}

// MuSig2SignSweep is a function that can be used to sign a sweep transaction
// cooperatively with the swap server.
type MuSig2SignSweep func(ctx context.Context,
	protocolVersion loopdb.ProtocolVersion, swapHash lntypes.Hash,
	paymentAddr [32]byte, nonce []byte, sweepTxPsbt []byte,
	prevoutMap map[wire.OutPoint]*wire.TxOut) (
	[]byte, []byte, error)

// MuSig2SignSweep is a function that can be used to sign a sweep transaction
// in a custom way.
type SignMuSig2 func(ctx context.Context, muSig2Version input.MuSig2Version,
	swapHash lntypes.Hash, rootHash chainhash.Hash, sigHash [32]byte,
) ([]byte, error)

// VerifySchnorrSig is a function that can be used to verify a schnorr
// signature.
type VerifySchnorrSig func(pubKey *btcec.PublicKey, hash, sig []byte) error

// SweepRequest is a request to sweep a specific outpoint.
type SweepRequest struct {
	// SwapHash is the hash of the swap that is being swept.
	SwapHash lntypes.Hash

	// Outpoint is the outpoint that is being swept.
	Outpoint wire.OutPoint

	// Value is the value of the outpoint that is being swept.
	Value btcutil.Amount

	// Notifier is a notifier that is used to notify the requester of this
	// sweep that the sweep was successful.
	Notifier *SpendNotifier
}

type SpendDetail struct {
	// Tx is the transaction that spent the outpoint.
	Tx *wire.MsgTx

	// OnChainFeePortion is the fee portion that was paid to get this sweep
	// confirmed on chain. This is the difference between the value of the
	// outpoint and the value of all sweeps that were included in the batch
	// divided by the number of sweeps.
	OnChainFeePortion btcutil.Amount
}

// SpendNotifier is a notifier that is used to notify the requester of a sweep
// that the sweep was successful.
type SpendNotifier struct {
	// SpendChan is a channel where the spend details are received.
	SpendChan chan *SpendDetail

	// SpendErrChan is a channel where spend errors are received.
	SpendErrChan chan error

	// QuitChan is a channel that can be closed to stop the notifier.
	QuitChan chan bool
}

var (
	ErrBatcherShuttingDown = errors.New("batcher shutting down")
)

// Batcher is a system that is responsible for accepting sweep requests and
// placing them in appropriate batches. It will spin up new batches as needed.
type Batcher struct {
	// batches is a map of batch IDs to the currently active batches.
	batches map[int32]*batch

	// sweepReqs is a channel where sweep requests are received.
	sweepReqs chan SweepRequest

	// errChan is a channel where errors are received.
	errChan chan error

	// quit signals that the batch must stop.
	quit chan struct{}

	// initDone is a channel that is closed when the batcher has been
	// initialized.
	initDone chan struct{}

	// wallet is the wallet kit client that is used by batches.
	wallet lndclient.WalletKitClient

	// chainNotifier is the chain notifier client that is used by batches.
	chainNotifier lndclient.ChainNotifierClient

	// signerClient is the signer client that is used by batches.
	signerClient lndclient.SignerClient

	// musig2ServerKit includes all the required functionality to collect
	// and verify signatures by the swap server in order to cooperatively
	// sweep funds.
	musig2ServerSign MuSig2SignSweep

	// verifySchnorrSig is a function that can be used to verify a schnorr
	// signature.
	VerifySchnorrSig VerifySchnorrSig

	// chainParams are the chain parameters of the chain that is used by
	// batches.
	chainParams *chaincfg.Params

	// store includes all the database interactions that are needed by the
	// batcher and the batches.
	store BatcherStore

	// sweepStore is used to load sweeps from the database.
	sweepStore SweepFetcher

	// wg is a waitgroup that is used to wait for all the goroutines to
	// exit.
	wg sync.WaitGroup

	// noBumping instructs sweepbatcher not to fee bump itself and rely on
	// external source of fee rates (MinFeeRate). To change the fee rate,
	// the caller has to update it in the source of SweepInfo (interface
	// SweepFetcher) and re-add the sweep by calling AddSweep.
	noBumping bool

	// signMuSig2 is a custom signer. If it is set, it is used to create
	// musig2 signatures instead of musig2SignSweep and signerClient. Note
	// that musig2SignSweep must be nil in this case, however signerClient
	// must still be provided, as it is used for non-coop spendings.
	signMuSig2 SignMuSig2
}

// BatcherConfig holds batcher configuration.
type BatcherConfig struct {
	// noBumping instructs sweepbatcher not to fee bump itself and rely on
	// external source of fee rates (MinFeeRate). To change the fee rate,
	// the caller has to update it in the source of SweepInfo (interface
	// SweepFetcher) and re-add the sweep by calling AddSweep.
	noBumping bool

	// signMuSig2 is a custom signer. If it is set, it is used to create
	// musig2 signatures instead of musig2SignSweep and signerClient. Note
	// that musig2SignSweep must be nil in this case, however signerClient
	// must still be provided, as it is used for non-coop spendings.
	signMuSig2 SignMuSig2
}

// BatcherOption configures batcher behaviour.
type BatcherOption func(*BatcherConfig)

// WithNoBumping instructs sweepbatcher not to fee bump itself and
// rely on external source of fee rates (MinFeeRate). To change the
// fee rate, the caller has to update it in the source of SweepInfo
// (interface SweepFetcher) and re-add the sweep by calling AddSweep.
func WithNoBumping() BatcherOption {
	return func(cfg *BatcherConfig) {
		cfg.noBumping = true
	}
}

// WithSignMuSig2 instructs sweepbatcher to use a custom function to
// produce MuSig2 signatures. If it is set, it is used to create
// musig2 signatures instead of musig2SignSweep and signerClient. Note
// that musig2SignSweep must be nil in this case, however signerClient
// must still be provided, as it is used for non-coop spendings.
func WithSignMuSig2(signMuSig2 SignMuSig2) BatcherOption {
	return func(cfg *BatcherConfig) {
		cfg.signMuSig2 = signMuSig2
	}
}

// NewBatcher creates a new Batcher instance.
func NewBatcher(wallet lndclient.WalletKitClient,
	chainNotifier lndclient.ChainNotifierClient,
	signerClient lndclient.SignerClient, musig2ServerSigner MuSig2SignSweep,
	verifySchnorrSig VerifySchnorrSig, chainparams *chaincfg.Params,
	store BatcherStore, sweepStore SweepFetcher,
	opts ...BatcherOption) *Batcher {

	var cfg BatcherConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.signMuSig2 != nil && musig2ServerSigner != nil {
		panic("signMuSig2 must not be used with musig2ServerSigner")
	}

	return &Batcher{
		batches:          make(map[int32]*batch),
		sweepReqs:        make(chan SweepRequest),
		errChan:          make(chan error, 1),
		quit:             make(chan struct{}),
		initDone:         make(chan struct{}),
		wallet:           wallet,
		chainNotifier:    chainNotifier,
		signerClient:     signerClient,
		musig2ServerSign: musig2ServerSigner,
		VerifySchnorrSig: verifySchnorrSig,
		chainParams:      chainparams,
		store:            store,
		sweepStore:       sweepStore,
		noBumping:        cfg.noBumping,
		signMuSig2:       cfg.signMuSig2,
	}
}

// Run starts the batcher and processes incoming sweep requests.
func (b *Batcher) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		close(b.quit)

		for _, batch := range b.batches {
			batch.Wait()
		}

		b.wg.Wait()
	}()

	// First we fetch all the batches that are not in a confirmed state from
	// the database. We will then resume the execution of these batches.
	batches, err := b.FetchUnconfirmedBatches(runCtx)
	if err != nil {
		return err
	}

	for _, batch := range batches {
		err := b.spinUpBatchFromDB(runCtx, batch)
		if err != nil {
			return err
		}
	}

	// Signal that the batcher has been initialized.
	close(b.initDone)

	for {
		select {
		case sweepReq := <-b.sweepReqs:
			sweep, err := b.fetchSweep(runCtx, sweepReq)
			if err != nil {
				return err
			}

			err = b.handleSweep(runCtx, sweep, sweepReq.Notifier)
			if err != nil {
				return err
			}

		case err := <-b.errChan:
			return err

		case <-runCtx.Done():
			return runCtx.Err()
		}
	}
}

// AddSweep adds a sweep request to the batcher for handling. This will either
// place the sweep in an existing batch or create a new one.
func (b *Batcher) AddSweep(sweepReq *SweepRequest) error {
	select {
	case b.sweepReqs <- *sweepReq:
		return nil

	case <-b.quit:
		return ErrBatcherShuttingDown
	}
}

// handleSweep handles a sweep request by either placing it in an existing
// batch, or by spinning up a new batch for it.
func (b *Batcher) handleSweep(ctx context.Context, sweep *sweep,
	notifier *SpendNotifier) error {

	completed, err := b.store.GetSweepStatus(ctx, sweep.swapHash)
	if err != nil {
		return err
	}

	log.Infof("Batcher handling sweep %x, completed=%v", sweep.swapHash[:6],
		completed)

	// If the sweep has already been completed in a confirmed batch then we
	// can't attach its notifier to the batch as that is no longer running.
	// Instead we directly detect and return the spend here.
	if completed && *notifier != (SpendNotifier{}) {
		return b.monitorSpendAndNotify(ctx, sweep, notifier)
	}

	sweep.notifier = notifier

	// Check if the sweep is already in a batch. If that is the case, we
	// provide the sweep to that batch and return.
	for _, batch := range b.batches {
		// This is a check to see if a batch is completed. In that case
		// we just lazily delete it and continue our scan.
		if batch.isComplete() {
			delete(b.batches, batch.id)
			continue
		}

		if batch.sweepExists(sweep.swapHash) {
			accepted, err := batch.addSweep(ctx, sweep)
			if err != nil && !errors.Is(err, ErrBatchShuttingDown) {
				return err
			}

			if !accepted {
				return fmt.Errorf("existing sweep %x was not "+
					"accepted by batch %d",
					sweep.swapHash[:6], batch.id)
			}

			// The sweep was updated in the batch, our job is done.
			return nil
		}
	}

	// If one of the batches accepts the sweep, we provide it to that batch.
	for _, batch := range b.batches {
		accepted, err := batch.addSweep(ctx, sweep)
		if err != nil && !errors.Is(err, ErrBatchShuttingDown) {
			return err
		}

		// If the sweep was accepted by this batch, we return, our job
		// is done.
		if accepted {
			return nil
		}
	}

	// If no batch is capable of accepting the sweep, we spin up a fresh
	// batch and hand the sweep over to it.
	batch, err := b.spinUpBatch(ctx)
	if err != nil {
		return err
	}

	// Add the sweep to the fresh batch.
	accepted, err := batch.addSweep(ctx, sweep)
	if err != nil {
		return err
	}

	// If the sweep wasn't accepted by the fresh batch something is wrong,
	// we should return the error.
	if !accepted {
		return fmt.Errorf("sweep %x was not accepted by new batch %d",
			sweep.swapHash[:6], batch.id)
	}

	return nil
}

// spinUpBatch spins up a new batch and returns it.
func (b *Batcher) spinUpBatch(ctx context.Context) (*batch, error) {
	cfg := batchConfig{
		maxTimeoutDistance: defaultMaxTimeoutDistance,
		noBumping:          b.noBumping,
		signMuSig2:         b.signMuSig2,
	}

	switch b.chainParams {
	case &chaincfg.MainNetParams:
		cfg.batchPublishDelay = defaultMainnetPublishDelay

	default:
		cfg.batchPublishDelay = defaultPublishDelay
	}

	batchKit := batchKit{
		returnChan:       b.sweepReqs,
		wallet:           b.wallet,
		chainNotifier:    b.chainNotifier,
		signerClient:     b.signerClient,
		musig2SignSweep:  b.musig2ServerSign,
		verifySchnorrSig: b.VerifySchnorrSig,
		purger:           b.AddSweep,
		store:            b.store,
		quit:             b.quit,
	}

	batch := NewBatch(cfg, batchKit)

	id, err := batch.insertAndAcquireID(ctx)
	if err != nil {
		return nil, err
	}

	// We add the batch to our map of batches and start it.
	b.batches[id] = batch

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()

		err := batch.Run(ctx)
		if err != nil {
			_ = b.writeToErrChan(ctx, err)
		}
	}()

	return batch, nil
}

// spinUpBatchDB spins up a batch that already existed in storage, then
// returns it.
func (b *Batcher) spinUpBatchFromDB(ctx context.Context, batch *batch) error {
	dbSweeps, err := b.store.FetchBatchSweeps(ctx, batch.id)
	if err != nil {
		return err
	}

	if len(dbSweeps) == 0 {
		log.Infof("skipping restored batch %d as it has no sweeps",
			batch.id)

		// It is safe to drop this empty batch as it has no sweeps.
		err := b.store.DropBatch(ctx, batch.id)
		if err != nil {
			log.Warnf("unable to drop empty batch %d: %v",
				batch.id, err)
		}

		return nil
	}

	primarySweep := dbSweeps[0]

	sweeps := make(map[lntypes.Hash]sweep)

	// Collect feeRate from sweeps and stored batch.
	feeRate := batch.rbfCache.FeeRate

	for _, dbSweep := range dbSweeps {
		sweep, err := b.convertSweep(ctx, dbSweep)
		if err != nil {
			return err
		}

		sweeps[sweep.swapHash] = *sweep

		// Set minFeeRate to max(sweep.minFeeRate) for all sweeps.
		if sweep.minFeeRate > feeRate {
			feeRate = sweep.minFeeRate
		}
	}

	rbfCache := rbfCache{
		LastHeight: batch.rbfCache.LastHeight,
		FeeRate:    feeRate,
	}

	logger := batchPrefixLogger(fmt.Sprintf("%d", batch.id))

	batchKit := batchKit{
		id:               batch.id,
		batchTxid:        batch.batchTxid,
		batchPkScript:    batch.batchPkScript,
		state:            batch.state,
		primaryID:        primarySweep.SwapHash,
		sweeps:           sweeps,
		rbfCache:         rbfCache,
		returnChan:       b.sweepReqs,
		wallet:           b.wallet,
		chainNotifier:    b.chainNotifier,
		signerClient:     b.signerClient,
		musig2SignSweep:  b.musig2ServerSign,
		verifySchnorrSig: b.VerifySchnorrSig,
		purger:           b.AddSweep,
		store:            b.store,
		log:              logger,
		quit:             b.quit,
	}

	cfg := batchConfig{
		maxTimeoutDistance: batch.cfg.maxTimeoutDistance,
		noBumping:          b.noBumping,
		signMuSig2:         b.signMuSig2,
	}

	newBatch, err := NewBatchFromDB(cfg, batchKit)
	if err != nil {
		return fmt.Errorf("failed in NewBatchFromDB: %w", err)
	}

	// We add the batch to our map of batches and start it.
	b.batches[batch.id] = newBatch

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()

		err := newBatch.Run(ctx)
		if err != nil {
			_ = b.writeToErrChan(ctx, err)
		}
	}()

	return nil
}

// FetchUnconfirmedBatches fetches all the batches from the database that are
// not in a confirmed state.
func (b *Batcher) FetchUnconfirmedBatches(ctx context.Context) ([]*batch,
	error) {

	dbBatches, err := b.store.FetchUnconfirmedSweepBatches(ctx)
	if err != nil {
		return nil, err
	}

	batches := make([]*batch, 0, len(dbBatches))
	for _, bch := range dbBatches {
		bch := bch

		batch := batch{}
		batch.id = bch.ID

		switch bch.State {
		case batchOpen:
			batch.state = Open

		case batchClosed:
			batch.state = Closed

		case batchConfirmed:
			batch.state = Confirmed
		}

		batch.batchTxid = &bch.BatchTxid
		batch.batchPkScript = bch.BatchPkScript

		rbfCache := rbfCache{
			LastHeight: bch.LastRbfHeight,
			FeeRate:    chainfee.SatPerKWeight(bch.LastRbfSatPerKw),
		}
		batch.rbfCache = rbfCache

		bchCfg := batchConfig{
			maxTimeoutDistance: bch.MaxTimeoutDistance,
			noBumping:          b.noBumping,
			signMuSig2:         b.signMuSig2,
		}
		batch.cfg = &bchCfg

		batches = append(batches, &batch)
	}

	return batches, nil
}

// monitorSpendAndNotify monitors the spend of a specific outpoint and writes
// the response back to the response channel.
func (b *Batcher) monitorSpendAndNotify(ctx context.Context, sweep *sweep,
	notifier *SpendNotifier) error {

	spendCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// First get the batch that completed the sweep.
	parentBatch, err := b.store.GetParentBatch(ctx, sweep.swapHash)
	if err != nil {
		return err
	}

	// Then we get the total amount that was swept by the batch.
	totalSwept, err := b.store.TotalSweptAmount(ctx, parentBatch.ID)
	if err != nil {
		return err
	}

	spendChan, spendErr, err := b.chainNotifier.RegisterSpendNtfn(
		spendCtx, &sweep.outpoint, sweep.htlc.PkScript,
		sweep.initiationHeight,
	)
	if err != nil {
		return err
	}

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		log.Infof("Batcher monitoring spend for swap %x",
			sweep.swapHash[:6])

		for {
			select {
			case spend := <-spendChan:
				spendTx := spend.SpendingTx
				// Calculate the fee portion that each sweep
				// should pay for the batch.
				feePortionPerSweep, roundingDifference :=
					getFeePortionForSweep(
						spendTx, len(spendTx.TxIn),
						totalSwept,
					)

				onChainFeePortion := getFeePortionPaidBySweep(
					spendTx, feePortionPerSweep,
					roundingDifference, sweep,
				)

				// Notify the requester of the spend
				// with the spend details, including the fee
				// portion for this particular sweep.
				spendDetail := &SpendDetail{
					Tx:                spendTx,
					OnChainFeePortion: onChainFeePortion,
				}

				select {
				case notifier.SpendChan <- spendDetail:
				case <-ctx.Done():
				}

				return

			case err := <-spendErr:
				select {
				case notifier.SpendErrChan <- err:
				case <-ctx.Done():
				}

				_ = b.writeToErrChan(ctx, err)
				return

			case <-notifier.QuitChan:
				return

			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

func (b *Batcher) writeToErrChan(ctx context.Context, err error) error {
	select {
	case b.errChan <- err:
		return nil

	case <-ctx.Done():
		return ctx.Err()
	}
}

// convertSweep converts a fetched sweep from the database to a sweep that is
// ready to be processed by the batcher. It loads swap from loopdb by calling
// method FetchLoopOutSwap.
func (b *Batcher) convertSweep(ctx context.Context, dbSweep *dbSweep) (
	*sweep, error) {

	s, err := b.sweepStore.FetchSweep(ctx, dbSweep.SwapHash)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch sweep data for %x: %w",
			dbSweep.SwapHash[:6], err)
	}

	return &sweep{
		swapHash:               dbSweep.SwapHash,
		outpoint:               dbSweep.Outpoint,
		value:                  dbSweep.Amount,
		confTarget:             s.ConfTarget,
		timeout:                s.Timeout,
		initiationHeight:       s.InitiationHeight,
		htlc:                   s.HTLC,
		preimage:               s.Preimage,
		swapInvoicePaymentAddr: s.SwapInvoicePaymentAddr,
		htlcKeys:               s.HTLCKeys,
		htlcSuccessEstimator:   s.HTLCSuccessEstimator,
		protocolVersion:        s.ProtocolVersion,
		isExternalAddr:         s.IsExternalAddr,
		destAddr:               s.DestAddr,
		minFeeRate:             s.MinFeeRate,
	}, nil
}

// LoopOutFetcher is used to load LoopOut swaps from the database.
// It is implemented by loopdb.SwapStore.
type LoopOutFetcher interface {
	// FetchLoopOutSwap returns the loop out swap with the given hash.
	FetchLoopOutSwap(ctx context.Context,
		hash lntypes.Hash) (*loopdb.LoopOut, error)
}

// SwapStoreWrapper is LoopOutFetcher wrapper providing SweepFetcher interface.
type SwapStoreWrapper struct {
	// swapStore is used to load LoopOut swaps from the database.
	swapStore LoopOutFetcher

	// chainParams are the chain parameters of the chain that is used by
	// batches.
	chainParams *chaincfg.Params
}

// FetchSweep returns details of the sweep with the given hash.
// Implements SweepFetcher interface.
func (f *SwapStoreWrapper) FetchSweep(ctx context.Context,
	swapHash lntypes.Hash) (*SweepInfo, error) {

	swap, err := f.swapStore.FetchLoopOutSwap(ctx, swapHash)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch loop out for %x: %w",
			swapHash[:6], err)
	}

	htlc, err := utils.GetHtlc(
		swapHash, &swap.Contract.SwapContract, f.chainParams,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get htlc: %w", err)
	}

	swapPaymentAddr, err := utils.ObtainSwapPaymentAddr(
		swap.Contract.SwapInvoice, f.chainParams,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get payment addr: %w", err)
	}

	return &SweepInfo{
		ConfTarget:             swap.Contract.SweepConfTarget,
		Timeout:                swap.Contract.CltvExpiry,
		InitiationHeight:       swap.Contract.InitiationHeight,
		HTLC:                   *htlc,
		Preimage:               swap.Contract.Preimage,
		SwapInvoicePaymentAddr: *swapPaymentAddr,
		HTLCKeys:               swap.Contract.HtlcKeys,
		HTLCSuccessEstimator:   htlc.AddSuccessToEstimator,
		ProtocolVersion:        swap.Contract.ProtocolVersion,
		IsExternalAddr:         swap.Contract.IsExternalAddr,
		DestAddr:               swap.Contract.DestAddr,
	}, nil
}

// NewSweepFetcherFromSwapStore accepts swapStore (e.g. loopdb) and returns
// a wrapper implementing SweepFetcher interface (suitable for NewBatcher).
func NewSweepFetcherFromSwapStore(swapStore LoopOutFetcher,
	chainParams *chaincfg.Params) (*SwapStoreWrapper, error) {

	return &SwapStoreWrapper{
		swapStore:   swapStore,
		chainParams: chainParams,
	}, nil
}

// fetchSweep fetches the sweep related information from the database.
func (b *Batcher) fetchSweep(ctx context.Context,
	sweepReq SweepRequest) (*sweep, error) {

	s, err := b.sweepStore.FetchSweep(ctx, sweepReq.SwapHash)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch sweep data for %x: %w",
			sweepReq.SwapHash[:6], err)
	}

	return &sweep{
		swapHash:               sweepReq.SwapHash,
		outpoint:               sweepReq.Outpoint,
		value:                  sweepReq.Value,
		confTarget:             s.ConfTarget,
		timeout:                s.Timeout,
		initiationHeight:       s.InitiationHeight,
		htlc:                   s.HTLC,
		preimage:               s.Preimage,
		swapInvoicePaymentAddr: s.SwapInvoicePaymentAddr,
		htlcKeys:               s.HTLCKeys,
		htlcSuccessEstimator:   s.HTLCSuccessEstimator,
		protocolVersion:        s.ProtocolVersion,
		isExternalAddr:         s.IsExternalAddr,
		destAddr:               s.DestAddr,
		minFeeRate:             s.MinFeeRate,
	}, nil
}
