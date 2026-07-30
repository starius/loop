// Package bip322 implements BIP-322 signing for Loop static addresses.
//
// The package is deliberately independent of Loop's managers, database, RPC
// server, and lnd clients. Callers provide immutable in-memory snapshots,
// execute the returned signing request with their signer, and pass the raw
// signatures back for local verification and finalization.
package bip322

import (
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/loop/staticaddr/version"
)

const (
	// MaxMessageBytes bounds memory use and the amount of opaque content
	// that the wallet will sign.
	MaxMessageBytes = 4 * 1024

	// MaxProofDeposits bounds proof size and lnd's quadratic Taproot sighash
	// work. The mandatory virtual challenge input is not included.
	MaxProofDeposits = 1000
)

// AddressParameters contains the static-address data required to construct and
// verify a proof.
type AddressParameters struct {
	// ClientPubKey authorizes the timeout path and participates in the
	// cooperative aggregate key.
	ClientPubKey *btcec.PublicKey

	// ServerPubKey is the other cooperative aggregate-key participant.
	ServerPubKey *btcec.PublicKey

	// Expiry is the timeout leaf's relative block delay.
	Expiry uint32

	// PkScript is the persisted P2TR output script.
	PkScript []byte

	// ProtocolVersion selects the static-address Taproot construction.
	ProtocolVersion version.AddressProtocolVersion

	// ChainParams identifies the address network.
	ChainParams *chaincfg.Params
}

// KnownDeposit is the database portion of a deposit snapshot. Its amount is
// compared with the wallet's authoritative unspent-output amount.
type KnownDeposit struct {
	// OutPoint identifies the persisted deposit.
	OutPoint wire.OutPoint

	// Amount is the value recorded in the deposit database.
	Amount btcutil.Amount
}

// Coin is the wallet portion of a deposit snapshot.
type Coin struct {
	// OutPoint identifies the wallet UTXO.
	OutPoint wire.OutPoint

	// Amount is the wallet's authoritative UTXO value.
	Amount btcutil.Amount

	// Confirmations is the wallet's current confirmation count.
	Confirmations int64

	// PkScript is the wallet's current output script.
	PkScript []byte
}

// Selection specifies which proof-of-funds inputs to include. Exactly one of
// All and Outpoints must be set.
type Selection struct {
	// All selects every eligible static-address wallet UTXO.
	All bool

	// Outpoints selects an explicit set when All is false.
	Outpoints []string
}

// SigningInput describes one Taproot script-path signature that the caller
// must obtain.
type SigningInput struct {
	// InputIndex is the transaction input to sign.
	InputIndex int

	// Output is the prevout committed to by the Taproot sighash.
	Output *wire.TxOut

	// WitnessScript is the static address's timeout leaf.
	WitnessScript []byte
}

// SigningRequest contains the complete transaction and prevout context needed
// by an external signer.
type SigningRequest struct {
	// Tx is a detached copy of the BIP-322 to_sign transaction.
	Tx *wire.MsgTx

	// PrevOutputs supplies the complete Taproot sighash prevout set.
	PrevOutputs []*wire.TxOut

	// Inputs describes each required timeout-path signature.
	Inputs []SigningInput
}

// Result is a locally verified BIP-322 signature and its cryptographically
// relevant metadata.
type Result struct {
	// Address is the static address that signs the message.
	Address string

	// Signature is the serialized, prefixed BIP-322 payload.
	Signature string

	// Constrained reports whether the proof has time or age constraints.
	Constrained bool

	// ValidAtTime is the absolute lock-time constraint.
	ValidAtTime uint32

	// ValidAtAge is the relative age constraint.
	ValidAtAge uint32

	// Coins lists real proof inputs in signed transaction order.
	Coins []Coin

	// TotalAmountSat is the checked sum of Coins.
	TotalAmountSat int64
}
