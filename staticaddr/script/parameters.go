package script

import (
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/loop/staticaddr/version"
	"github.com/lightningnetwork/lnd/keychain"
)

const (
	// MaxCSVExpiry caps a static-address delay at 200 days, assuming 144
	// blocks per day.
	MaxCSVExpiry = uint32(200 * 144)
)

// ValidateExpiry validates a persisted static-address CSV expiry. Persisted
// values are checked again before signing because older or corrupt database
// rows may not have passed current address-creation validation.
func ValidateExpiry(expiry uint32) error {
	switch {
	case expiry == 0:
		return fmt.Errorf("static address CSV expiry must be non-zero")

	case expiry&^wire.SequenceLockTimeMask != 0:
		return fmt.Errorf("static address expiry does not fit into CSV: %x",
			expiry)

	case expiry > MaxCSVExpiry:
		return fmt.Errorf("static address CSV expiry %v exceeds maximum %v",
			expiry, MaxCSVExpiry)

	default:
		return nil
	}
}

// Parameters holds all the necessary information for the 2-of-2 multisig
// address.
type Parameters struct {
	// ClientPubkey is the client's pubkey for the static address. It is
	// used for the 2-of-2 funding output as well as for the client's
	// timeout path.
	ClientPubkey *btcec.PublicKey

	// ServerPubkey is the server's pubkey for the static address. It is
	// used for the 2-of-2 funding output.
	ServerPubkey *btcec.PublicKey

	// Expiry is the CSV timeout value at which the client can claim the
	// static address's timeout path.
	Expiry uint32

	// PkScript is the unique static address's output script.
	PkScript []byte

	// KeyLocator is the locator of the client's key.
	KeyLocator keychain.KeyLocator

	// ProtocolVersion is the protocol version of the static address.
	ProtocolVersion version.AddressProtocolVersion

	// InitiationHeight is the height at which the address was initiated.
	InitiationHeight int32
}
