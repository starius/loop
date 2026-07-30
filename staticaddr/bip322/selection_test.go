package bip322

import (
	"bytes"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/stretchr/testify/require"
)

// testOutpoint creates a compact, deterministic outpoint for selection tests.
func testOutpoint(id byte, index uint32) wire.OutPoint {
	return wire.OutPoint{
		Hash:  chainhash.Hash{id},
		Index: index,
	}
}

// testCoin creates a confirmed wallet coin for the supplied static address.
func testCoin(outpoint wire.OutPoint, amount btcutil.Amount,
	pkScript []byte) Coin {

	return Coin{
		OutPoint:      outpoint,
		Amount:        amount,
		Confirmations: 2,
		PkScript:      pkScript,
	}
}

// TestSelectCoins checks explicit and all selection, BIP-69 ordering, and
// defensive script copies.
func TestSelectCoins(t *testing.T) {
	t.Parallel()

	pkScript := []byte{0x51, 0x20, 1}
	firstOutpoint := testOutpoint(2, 4)
	secondOutpoint := testOutpoint(1, 7)
	firstCoin := testCoin(firstOutpoint, 20_000, pkScript)
	secondCoin := testCoin(secondOutpoint, 30_000, pkScript)
	known := []KnownDeposit{
		{OutPoint: firstOutpoint, Amount: firstCoin.Amount},
		{OutPoint: secondOutpoint, Amount: secondCoin.Amount},
	}

	selected, err := SelectCoins(
		pkScript, known, []Coin{firstCoin, secondCoin}, Selection{
			Outpoints: []string{
				firstOutpoint.String(), secondOutpoint.String(),
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, selected, 2)
	require.Equal(t, secondOutpoint, selected[0].OutPoint)
	require.Equal(t, firstOutpoint, selected[1].OutPoint)

	all, err := SelectCoins(
		pkScript, known, []Coin{firstCoin, secondCoin},
		Selection{All: true},
	)
	require.NoError(t, err)
	require.Equal(t, selected, all)

	selected[0].PkScript[0] ^= 1
	require.Equal(t, pkScript, all[0].PkScript)
}

// TestSelectCoinsValidation covers malformed, ambiguous, stale, and mismatched
// wallet and database snapshots.
func TestSelectCoinsValidation(t *testing.T) {
	t.Parallel()

	pkScript := []byte{0x51, 0x20, 1}
	outpoint := testOutpoint(1, 2)
	coin := testCoin(outpoint, 20_000, pkScript)
	known := []KnownDeposit{{
		OutPoint: outpoint,
		Amount:   coin.Amount,
	}}

	// Each case isolates one condition that must fail before signing.
	tests := []struct {
		name      string
		known     []KnownDeposit
		unspent   []Coin
		selection Selection
		error     string
	}{
		{
			name:      "neither selection",
			known:     known,
			unspent:   []Coin{coin},
			selection: Selection{},
			error:     ErrInvalidSelection.Error(),
		},
		{
			name:    "both selections",
			known:   known,
			unspent: []Coin{coin},
			selection: Selection{
				All:       true,
				Outpoints: []string{outpoint.String()},
			},
			error: ErrInvalidSelection.Error(),
		},
		{
			name:    "empty all",
			known:   known,
			unspent: nil,
			selection: Selection{
				All: true,
			},
			error: "selection is empty",
		},
		{
			name:    "malformed outpoint",
			known:   known,
			unspent: []Coin{coin},
			selection: Selection{
				Outpoints: []string{"not-an-outpoint"},
			},
			error: "invalid outpoint at index 0",
		},
		{
			name:    "duplicate requested outpoint",
			known:   known,
			unspent: []Coin{coin},
			selection: Selection{
				Outpoints: []string{
					outpoint.String(), outpoint.String(),
				},
			},
			error: "duplicate outpoint",
		},
		{
			name: "duplicate database record",
			known: []KnownDeposit{
				known[0], known[0],
			},
			unspent: []Coin{coin},
			selection: Selection{
				Outpoints: []string{outpoint.String()},
			},
			error: "duplicate deposit record",
		},
		{
			name:  "duplicate wallet UTXO",
			known: known,
			unspent: []Coin{
				coin, coin,
			},
			selection: Selection{
				Outpoints: []string{outpoint.String()},
			},
			error: "duplicate wallet UTXO",
		},
		{
			name:    "unknown deposit",
			known:   nil,
			unspent: []Coin{coin},
			selection: Selection{
				Outpoints: []string{outpoint.String()},
			},
			error: "unknown deposit",
		},
		{
			name:    "spent deposit",
			known:   known,
			unspent: nil,
			selection: Selection{
				Outpoints: []string{outpoint.String()},
			},
			error: "not a confirmed unspent wallet output",
		},
		{
			name:  "unconfirmed deposit",
			known: known,
			unspent: []Coin{func() Coin {
				changed := coin
				changed.Confirmations = 0
				return changed
			}()},
			selection: Selection{
				Outpoints: []string{outpoint.String()},
			},
			error: "is unconfirmed",
		},
		{
			name:  "zero amount",
			known: known,
			unspent: []Coin{func() Coin {
				changed := coin
				changed.Amount = 0
				return changed
			}()},
			selection: Selection{
				Outpoints: []string{outpoint.String()},
			},
			error: "invalid value",
		},
		{
			name:  "amount above maximum",
			known: known,
			unspent: []Coin{func() Coin {
				changed := coin
				changed.Amount = btcutil.MaxSatoshi + 1
				return changed
			}()},
			selection: Selection{
				Outpoints: []string{outpoint.String()},
			},
			error: "invalid value",
		},
		{
			name:  "wrong script",
			known: known,
			unspent: []Coin{func() Coin {
				changed := coin
				changed.PkScript = []byte{0}
				return changed
			}()},
			selection: Selection{
				Outpoints: []string{outpoint.String()},
			},
			error: "unexpected script",
		},
		{
			name: "database amount mismatch",
			known: []KnownDeposit{{
				OutPoint: outpoint,
				Amount:   coin.Amount + 1,
			}},
			unspent: []Coin{coin},
			selection: Selection{
				Outpoints: []string{outpoint.String()},
			},
			error: "value mismatch: wallet=20000 database=20001",
		},
		{
			name:    "wallet orphan in all selection",
			known:   nil,
			unspent: []Coin{coin},
			selection: Selection{
				All: true,
			},
			error: "has no deposit record",
		},
		{
			name:  "null txid",
			known: nil,
			unspent: []Coin{{
				OutPoint:      wire.OutPoint{Index: 1},
				Amount:        1,
				Confirmations: 1,
				PkScript:      pkScript,
			}},
			selection: Selection{
				Outpoints: []string{
					(wire.OutPoint{Index: 1}).String(),
				},
			},
			error: "invalid proof outpoint",
		},
		{
			name:  "maximum index",
			known: nil,
			unspent: []Coin{{
				OutPoint: wire.OutPoint{
					Hash:  chainhash.Hash{1},
					Index: wire.MaxPrevOutIndex,
				},
				Amount:        1,
				Confirmations: 1,
				PkScript:      pkScript,
			}},
			selection: Selection{
				Outpoints: []string{(wire.OutPoint{
					Hash:  chainhash.Hash{1},
					Index: wire.MaxPrevOutIndex,
				}).String()},
			},
			error: "invalid proof outpoint",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := SelectCoins(
				pkScript, test.known, test.unspent,
				test.selection,
			)
			require.ErrorContains(t, err, test.error)
		})
	}

	tooMany := make([]string, MaxProofDeposits+1)
	for idx := range tooMany {
		tooMany[idx] = outpoint.String()
	}
	_, err := SelectCoins(pkScript, known, []Coin{coin}, Selection{
		Outpoints: tooMany,
	})
	require.ErrorContains(t, err, "too many proof deposits")
	require.ErrorIs(t, err, ErrTooManyDeposits)

	_, err = SelectCoins(pkScript, known, []Coin{coin}, Selection{
		Outpoints: []string{"not-an-outpoint"},
	})
	require.ErrorIs(t, err, ErrInvalidSelection)

	_, err = SelectCoins(pkScript, known, nil, Selection{
		Outpoints: []string{outpoint.String()},
	})
	require.ErrorIs(t, err, ErrDepositUnavailable)

	_, err = SelectCoins(pkScript, nil, []Coin{coin}, Selection{
		All: true,
	})
	require.ErrorIs(t, err, ErrSnapshotChanged)
}

// TestSelectCoinsAllowsBusyAddress prevents the all-deposits path from
// regressing to the former 100-input limit.
func TestSelectCoinsAllowsBusyAddress(t *testing.T) {
	t.Parallel()

	const depositCount = 101
	pkScript := []byte{0x51, 0x20, 1}
	known := make([]KnownDeposit, depositCount)
	unspent := make([]Coin, depositCount)
	for idx := range depositCount {
		outpoint := testOutpoint(byte(idx+1), uint32(idx))
		amount := btcutil.Amount(idx + 1)
		known[idx] = KnownDeposit{
			OutPoint: outpoint,
			Amount:   amount,
		}
		unspent[idx] = testCoin(outpoint, amount, pkScript)
	}

	selected, err := SelectCoins(
		pkScript, known, unspent, Selection{All: true},
	)
	require.NoError(t, err)
	require.Len(t, selected, depositCount)
}

// TestValidateStillUnspent checks the post-signing freshness guard against
// disappearance, replacement, reorg, and duplicate wallet entries.
func TestValidateStillUnspent(t *testing.T) {
	t.Parallel()

	pkScript := []byte{0x51, 0x20, 1}
	outpoint := testOutpoint(1, 2)
	selected := []Coin{testCoin(outpoint, 20_000, pkScript)}

	require.NoError(t, ValidateStillUnspent(selected, selected))

	err := ValidateStillUnspent(selected, nil)
	require.ErrorContains(t, err, "was spent")
	require.ErrorIs(t, err, ErrSnapshotChanged)

	changedAmount := selected[0]
	changedAmount.Amount++
	err = ValidateStillUnspent(selected, []Coin{changedAmount})
	require.ErrorContains(t, err, "changed during proof")

	changedScript := selected[0]
	changedScript.PkScript = bytes.Repeat([]byte{1}, len(pkScript))
	err = ValidateStillUnspent(selected, []Coin{changedScript})
	require.ErrorContains(t, err, "changed during proof")

	unconfirmed := selected[0]
	unconfirmed.Confirmations = 0
	err = ValidateStillUnspent(selected, []Coin{unconfirmed})
	require.ErrorContains(t, err, "changed during proof")

	err = ValidateStillUnspent(selected, []Coin{selected[0], selected[0]})
	require.ErrorContains(t, err, "duplicate wallet UTXO")
}

// TestSelectCoinsRejectsOversizedSelectionBeforeParsing ensures the size cap
// bounds work even when every supplied outpoint string is malformed.
func TestSelectCoinsRejectsOversizedSelectionBeforeParsing(t *testing.T) {
	t.Parallel()

	selection := Selection{
		Outpoints: make([]string, MaxProofDeposits+1),
	}
	for idx := range selection.Outpoints {
		selection.Outpoints[idx] = strings.Repeat("z", idx+1)
	}

	_, err := SelectCoins(nil, nil, nil, selection)
	require.ErrorContains(t, err, "too many proof deposits")
}
