package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/loop/looprpc"
	"github.com/lightninglabs/loop/staticaddr/deposit"
	"github.com/lightninglabs/loop/staticaddr/loopin"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

const (
	// confirmedSmallOutpoint labels the smaller confirmed selection fixture.
	confirmedSmallOutpoint = "confirmed-small"

	// mempoolLargeOutpoint labels the larger unconfirmed selection fixture.
	mempoolLargeOutpoint = "mempool-large"
)

// TestBip322Message checks exclusive message sources, exact file bytes, UTF-8,
// and the shared size bound.
func TestBip322Message(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, args ...string) (string, error) {
		t.Helper()

		var message string
		command := &cli.Command{
			Name:  "test",
			Flags: bip322MessageFlags(),
			Action: func(_ context.Context, cmd *cli.Command) error {
				var err error
				message, err = bip322Message(cmd)
				return err
			},
		}

		err := command.Run(context.Background(), append([]string{
			"test",
		}, args...))
		return message, err
	}

	message, err := run(t, "--message", "exact message")
	require.NoError(t, err)
	require.Equal(t, "exact message", message)

	messageFile := filepath.Join(t.TempDir(), "message")
	require.NoError(t, os.WriteFile(
		messageFile, []byte("line one\nline two\n"), 0o600,
	))
	message, err = run(t, "--message_file", messageFile)
	require.NoError(t, err)
	require.Equal(t, "line one\nline two\n", message)

	_, err = run(
		t, "--message", "one", "--message_file", messageFile,
	)
	require.ErrorContains(t, err, "exactly one")

	invalidFile := filepath.Join(t.TempDir(), "invalid")
	require.NoError(t, os.WriteFile(
		invalidFile, []byte{0xff}, 0o600,
	))
	_, err = run(t, "--message_file", invalidFile)
	require.ErrorContains(t, err, "valid UTF-8")

	_, err = run(t, "--message", strings.Repeat(
		"a", bip322MaxMessageBytes+1,
	))
	require.ErrorContains(t, err, "too large")
}

// TestLowConfDepositWarningConfirmedOnly verifies confirmed deposits below the
// conservative warning threshold are included in the warning text.
func TestLowConfDepositWarningConfirmedOnly(t *testing.T) {
	t.Parallel()

	deposits := []*looprpc.Deposit{
		{
			Outpoint:           "confirmed-low",
			ConfirmationHeight: 100,
			BlocksUntilExpiry:  140,
		},
		{
			Outpoint:           "confirmed-high",
			ConfirmationHeight: 95,
			BlocksUntilExpiry:  139,
		},
	}

	warning := lowConfDepositWarning(
		deposits, []string{"confirmed-low", "confirmed-high"}, 144,
	)

	require.Contains(t, warning, "confirmed-low (5 confirmations)")
	require.NotContains(t, warning, "confirmed-high")
}

// TestLowConfDepositWarningUnconfirmed verifies unconfirmed deposits get a
// warning that the swap may wait for confirmation-risk acceptance.
func TestLowConfDepositWarningUnconfirmed(t *testing.T) {
	t.Parallel()

	deposits := []*looprpc.Deposit{
		{
			Outpoint:           "mempool",
			ConfirmationHeight: 0,
			BlocksUntilExpiry:  144,
		},
	}

	warning := lowConfDepositWarning(deposits, []string{"mempool"}, 144)

	require.Contains(t, warning, "mempool (unconfirmed)")
	require.True(
		t,
		strings.Contains(
			warning,
			"conservative 6-confirmation threshold",
		),
	)
	require.NotContains(t, warning, "executed immediately")
}

// TestWarningDepositOutpointsAutoSelectPrefersConfirmed verifies automatic
// warning selection keeps the loop-in preference for confirmed outputs.
func TestWarningDepositOutpointsAutoSelectPrefersConfirmed(t *testing.T) {
	t.Parallel()

	const csvExpiry = 1100

	deposits := []*looprpc.Deposit{
		{
			Outpoint:           mempoolLargeOutpoint,
			Value:              2_000_000,
			ConfirmationHeight: 0,
			BlocksUntilExpiry:  csvExpiry,
		},
		{
			Outpoint:           "confirmed",
			Value:              1_500_000,
			ConfirmationHeight: 100,
			BlocksUntilExpiry:  csvExpiry - 5,
		},
	}

	selected := warningDepositOutpoints(deposits, nil, true, 1_000_000)

	require.Equal(t, []string{"confirmed"}, selected)
	require.Empty(t, lowConfDepositWarning(deposits, selected, csvExpiry))
}

// TestWarningDepositOutpointsAutoSelectIncludesNeededUnconfirmed verifies the
// warning path includes mempool deposits when they are needed for the target.
func TestWarningDepositOutpointsAutoSelectIncludesNeededUnconfirmed(t *testing.T) {
	t.Parallel()

	const csvExpiry = 1100

	deposits := []*looprpc.Deposit{
		{
			Outpoint:           confirmedSmallOutpoint,
			Value:              500_000,
			ConfirmationHeight: 100,
			BlocksUntilExpiry:  csvExpiry - 5,
		},
		{
			Outpoint:           mempoolLargeOutpoint,
			Value:              2_000_000,
			ConfirmationHeight: 0,
			BlocksUntilExpiry:  csvExpiry,
		},
	}

	selected := warningDepositOutpoints(deposits, nil, true, 1_000_000)

	require.Equal(
		t, []string{confirmedSmallOutpoint, mempoolLargeOutpoint}, selected,
	)

	warning := lowConfDepositWarning(deposits, selected, csvExpiry)
	require.Contains(t, warning, mempoolLargeOutpoint+" (unconfirmed)")
	require.NotContains(t, warning, confirmedSmallOutpoint)
}

// TestWarningDepositSelectionMatchesLoopInSelection verifies CLI warning
// selection matches the loop-in selector.
func TestWarningDepositSelectionMatchesLoopInSelection(t *testing.T) {
	t.Parallel()

	const (
		blockHeight  = uint32(10_000)
		csvExpiry    = uint32(1_200)
		targetAmount = int64(2_500_000)
	)

	type fixture struct {
		name               string
		value              int64
		confirmationHeight int64
	}

	fixtures := []fixture{
		{
			name:               "mempool-huge",
			value:              3_000_000,
			confirmationHeight: 0,
		},
		{
			name:               "confirmed-later-expiry",
			value:              2_000_000,
			confirmationHeight: 9_900,
		},
		{
			name:               "confirmed-earlier-expiry",
			value:              2_000_000,
			confirmationHeight: 9_890,
		},
		{
			name:               confirmedSmallOutpoint,
			value:              600_000,
			confirmationHeight: 9_900,
		},
		{
			name:               "confirmed-too-close-to-expiry",
			value:              5_000_000,
			confirmationHeight: 9_849,
		},
	}

	rpcDeposits := make([]*looprpc.Deposit, 0, len(fixtures))
	loopInDeposits := make([]*deposit.Deposit, 0, len(fixtures))
	for idx, fixture := range fixtures {
		hash := chainhash.Hash{byte(idx + 1)}
		outpoint := wire.OutPoint{
			Hash:  hash,
			Index: uint32(idx),
		}

		blocksUntilExpiry := int64(0)
		if fixture.confirmationHeight > 0 {
			blocksUntilExpiry = fixture.confirmationHeight +
				int64(csvExpiry) - int64(blockHeight)
		}

		rpcDeposits = append(rpcDeposits, &looprpc.Deposit{
			Outpoint:           outpoint.String(),
			Value:              fixture.value,
			ConfirmationHeight: fixture.confirmationHeight,
			BlocksUntilExpiry:  blocksUntilExpiry,
		})
		loopInDeposits = append(loopInDeposits, &deposit.Deposit{
			OutPoint:           outpoint,
			Value:              btcutil.Amount(fixture.value),
			ConfirmationHeight: fixture.confirmationHeight,
		})
	}

	cliSelected := autoSelectedWarningOutpoints(
		rpcDeposits, targetAmount,
	)

	loopInSelected, err := loopin.SelectDeposits(
		btcutil.Amount(targetAmount), loopInDeposits, csvExpiry,
		blockHeight,
	)
	require.NoError(t, err)

	loopInSelectedOutpoints := make([]string, 0, len(loopInSelected))
	for _, selected := range loopInSelected {
		loopInSelectedOutpoints = append(
			loopInSelectedOutpoints, selected.OutPoint.String(),
		)
	}

	require.Equal(t, loopInSelectedOutpoints, cliSelected)
}
