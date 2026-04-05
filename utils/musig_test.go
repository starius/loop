package utils

import (
	"testing"

	"github.com/lightninglabs/loop/test"
	"github.com/lightningnetwork/lnd/input"
	"github.com/stretchr/testify/require"
)

// rawKeys returns serialized private keys for the given test seeds.
func rawKeys(seeds ...int32) [][32]byte {
	keys := make([][32]byte, len(seeds))
	for i, seed := range seeds {
		privKey, _ := test.CreateKey(seed)
		copy(keys[i][:], privKey.Serialize())
	}

	return keys
}

// TestMuSig2SignRejectsSingleSigner ensures the helper fails fast with a clear
// error instead of entering an invalid one-party MuSig2 flow.
func TestMuSig2SignRejectsSingleSigner(t *testing.T) {
	_, err := MuSig2Sign(
		input.MuSig2Version100RC2,
		rawKeys(1),
		&input.MuSig2Tweaks{},
		[32]byte{},
	)
	require.ErrorContains(t, err, "need at least two signing keys")
}

// TestMuSig2SignSupportsVersions verifies the helper works with the supported
// MuSig2 versions used in Loop.
func TestMuSig2SignSupportsVersions(t *testing.T) {
	t.Parallel()

	for _, version := range []input.MuSig2Version{
		input.MuSig2Version040,
		input.MuSig2Version100RC2,
	} {
		t.Run(testVersionName(version), func(t *testing.T) {
			t.Parallel()

			sig, err := MuSig2Sign(
				version, rawKeys(1, 2), &input.MuSig2Tweaks{},
				[32]byte{},
			)
			require.NoError(t, err)
			require.Len(t, sig, 64)
		})
	}
}

// testVersionName returns a stable subtest name for a MuSig2 version.
func testVersionName(version input.MuSig2Version) string {
	switch version {
	case input.MuSig2Version040:
		return "MuSig2 0.4"

	case input.MuSig2Version100RC2:
		return "MuSig2 1.0RC2"

	default:
		return "unknown"
	}
}
