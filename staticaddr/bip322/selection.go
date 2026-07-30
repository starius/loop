package bip322

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/txsort"
	"github.com/btcsuite/btcd/wire"
)

var (
	// ErrInvalidSelection is returned when a proof-of-funds request does
	// not contain a valid, unambiguous deposit selection.
	ErrInvalidSelection = errors.New(
		"invalid proof deposit selection",
	)

	// ErrTooManyDeposits is returned when one proof would exceed the
	// operational signing-work limit.
	ErrTooManyDeposits = errors.New("too many proof deposits")

	// ErrDepositUnavailable is returned when a requested deposit is not a
	// confirmed wallet UTXO at selection time.
	ErrDepositUnavailable = errors.New("proof deposit is unavailable")

	// ErrSnapshotChanged is returned when wallet and database snapshots
	// cannot describe one stable set of proof inputs.
	ErrSnapshotChanged = errors.New("proof deposit snapshot changed")
)

// SelectCoins validates database and wallet snapshots, then returns the
// selected wallet coins in canonical BIP-69 order. The wallet amount is
// authoritative, but must exactly match the database amount.
func SelectCoins(pkScript []byte, known []KnownDeposit, unspent []Coin,
	selection Selection) ([]Coin, error) {

	if selection.All == (len(selection.Outpoints) > 0) {
		return nil, fmt.Errorf("%w: select exactly one of explicit "+
			"outpoints or all deposits", ErrInvalidSelection)
	}
	if len(selection.Outpoints) > MaxProofDeposits {
		return nil, fmt.Errorf("%w: got %d, max %d",
			ErrTooManyDeposits, len(selection.Outpoints),
			MaxProofDeposits)
	}

	// Index both snapshots while rejecting ambiguity at their trust boundary.
	knownByOutpoint := make(map[wire.OutPoint]KnownDeposit, len(known))
	for _, deposit := range known {
		if _, ok := knownByOutpoint[deposit.OutPoint]; ok {
			return nil, fmt.Errorf("duplicate deposit record for %v",
				deposit.OutPoint)
		}

		knownByOutpoint[deposit.OutPoint] = deposit
	}

	unspentByOutpoint := make(map[wire.OutPoint]Coin, len(unspent))
	for _, coin := range unspent {
		if _, ok := unspentByOutpoint[coin.OutPoint]; ok {
			return nil, fmt.Errorf("duplicate wallet UTXO for %v",
				coin.OutPoint)
		}

		unspentByOutpoint[coin.OutPoint] = cloneCoin(coin)
	}

	var outpoints []wire.OutPoint
	if selection.All {
		// --all means all eligible wallet UTXOs, not all historical database
		// records. Every wallet coin must still have known provenance.
		outpoints = make([]wire.OutPoint, 0, len(unspentByOutpoint))
		for outpoint := range unspentByOutpoint {
			if _, ok := knownByOutpoint[outpoint]; !ok {
				return nil, fmt.Errorf("%w: wallet UTXO %v has no "+
					"deposit record", ErrSnapshotChanged,
					outpoint)
			}

			outpoints = append(outpoints, outpoint)
		}
	} else {
		// Parse explicit strings once and reject duplicates before lookup.
		outpoints = make([]wire.OutPoint, 0, len(selection.Outpoints))
		seen := make(map[wire.OutPoint]struct{}, len(selection.Outpoints))
		for idx, requested := range selection.Outpoints {
			outpoint, err := wire.NewOutPointFromString(requested)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid outpoint at "+
					"index %d: %v", ErrInvalidSelection, idx,
					err)
			}
			if _, ok := seen[*outpoint]; ok {
				return nil, fmt.Errorf("%w: duplicate outpoint %v",
					ErrInvalidSelection, outpoint)
			}

			seen[*outpoint] = struct{}{}
			outpoints = append(outpoints, *outpoint)
		}
	}

	switch {
	case len(outpoints) == 0:
		return nil, fmt.Errorf("%w: proof selection is empty",
			ErrInvalidSelection)

	case len(outpoints) > MaxProofDeposits:
		return nil, fmt.Errorf("%w: got %d, max %d",
			ErrTooManyDeposits, len(outpoints), MaxProofDeposits)
	}

	sortOutpointsBIP69(outpoints)

	// Join wallet and database views after ordering. Wallet data is current
	// and authoritative, while equality detects stale or corrupt DB values.
	selected := make([]Coin, 0, len(outpoints))
	for _, outpoint := range outpoints {
		if err := validateOutpoint(outpoint); err != nil {
			return nil, err
		}

		knownDeposit, ok := knownByOutpoint[outpoint]
		if !ok {
			return nil, fmt.Errorf("%w: unknown deposit %v",
				ErrInvalidSelection, outpoint)
		}

		coin, ok := unspentByOutpoint[outpoint]
		if !ok {
			return nil, fmt.Errorf("%w: deposit %v is not a confirmed "+
				"unspent wallet output", ErrDepositUnavailable,
				outpoint)
		}
		if err := validateCoin(pkScript, coin); err != nil {
			return nil, err
		}
		if coin.Amount != knownDeposit.Amount {
			return nil, fmt.Errorf("deposit %v value mismatch: wallet=%d "+
				"database=%d", outpoint, coin.Amount,
				knownDeposit.Amount)
		}

		selected = append(selected, cloneCoin(coin))
	}

	return selected, nil
}

// ValidateStillUnspent checks a second wallet snapshot after signing. It
// rejects any selected coin that disappeared or whose signed prevout data no
// longer matches the wallet.
func ValidateStillUnspent(selected, current []Coin) error {
	// Build a fresh wallet view after signing to narrow the race in which a
	// selected coin is spent or replaced while the proof is constructed.
	currentByOutpoint := make(map[wire.OutPoint]Coin, len(current))
	for _, coin := range current {
		if _, ok := currentByOutpoint[coin.OutPoint]; ok {
			return fmt.Errorf("%w: duplicate wallet UTXO %v during "+
				"proof recheck", ErrSnapshotChanged,
				coin.OutPoint)
		}

		currentByOutpoint[coin.OutPoint] = coin
	}

	for _, selectedCoin := range selected {
		currentCoin, ok := currentByOutpoint[selectedCoin.OutPoint]
		if !ok {
			return fmt.Errorf("%w: deposit %v was spent during proof "+
				"construction", ErrSnapshotChanged,
				selectedCoin.OutPoint)
		}
		if currentCoin.Amount != selectedCoin.Amount ||
			currentCoin.Confirmations < 1 ||
			!bytes.Equal(currentCoin.PkScript, selectedCoin.PkScript) {

			return fmt.Errorf("%w: deposit %v changed during proof "+
				"construction", ErrSnapshotChanged,
				selectedCoin.OutPoint)
		}
	}

	return nil
}

// validateOutpoint rejects sentinel values that cannot identify real coins.
func validateOutpoint(outpoint wire.OutPoint) error {
	if outpoint.Hash == (wire.OutPoint{}).Hash ||
		outpoint.Index == wire.MaxPrevOutIndex {

		return fmt.Errorf("%w: invalid proof outpoint %v",
			ErrInvalidSelection, outpoint)
	}

	return nil
}

// validateCoin checks the wallet facts required by a proof input.
func validateCoin(pkScript []byte, coin Coin) error {
	switch {
	case coin.Confirmations < 1:
		return fmt.Errorf("%w: deposit %v is unconfirmed",
			ErrDepositUnavailable, coin.OutPoint)

	case coin.Amount <= 0 || coin.Amount > btcutil.MaxSatoshi:
		return fmt.Errorf("deposit %v has invalid value %d",
			coin.OutPoint, coin.Amount)

	case !bytes.Equal(coin.PkScript, pkScript):
		return fmt.Errorf("deposit %v has unexpected script",
			coin.OutPoint)

	default:
		return nil
	}
}

// cloneCoin copies a coin and its mutable output script.
func cloneCoin(coin Coin) Coin {
	coin.PkScript = bytes.Clone(coin.PkScript)
	return coin
}

// sortOutpointsBIP69 applies btcd's canonical transaction input ordering to
// an outpoint slice.
func sortOutpointsBIP69(outpoints []wire.OutPoint) {
	tx := wire.NewMsgTx(2)
	for _, outpoint := range outpoints {
		tx.AddTxIn(wire.NewTxIn(&outpoint, nil, nil))
	}

	txsort.InPlaceSort(tx)
	for idx := range tx.TxIn {
		outpoints[idx] = tx.TxIn[idx].PreviousOutPoint
	}
}
