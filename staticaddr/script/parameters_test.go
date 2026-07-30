package script

import (
	"testing"

	"github.com/btcsuite/btcd/wire"
	"github.com/stretchr/testify/require"
)

// TestValidateExpiry checks every valid boundary and forbidden CSV flag.
func TestValidateExpiry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		expiry uint32
		valid  bool
	}{
		{
			name:   "minimum",
			expiry: 1,
			valid:  true,
		},
		{
			name:   "maximum",
			expiry: MaxCSVExpiry,
			valid:  true,
		},
		{
			name:   "zero",
			expiry: 0,
		},
		{
			name:   "above maximum",
			expiry: MaxCSVExpiry + 1,
		},
		{
			name:   "disable flag",
			expiry: wire.SequenceLockTimeDisabled,
		},
		{
			name:   "time based",
			expiry: wire.SequenceLockTimeIsSeconds | 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateExpiry(test.expiry)
			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
