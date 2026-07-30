# Static-address message signing and proof of funds

Loop can sign a message with a static address or prove ownership of funds held
at that address. It uses the client-controlled timeout path and the generic
BIP-322 signature format. Signing is performed by `loopd`; the `loop` command
does not connect to lnd directly.

## Commands

Sign a message with the static address:

```text
loop static bip322 ful --message "example.com login nonce=..."
```

Prove ownership of funds held at the static address, using either explicit
deposits or every confirmed deposit that lnd currently reports unspent:

```text
loop static bip322 pof --message "audit.example challenge=..." \
    --utxo <txid:vout> [--utxo <txid:vout> ...]

loop static bip322 pof --message_file challenge.txt --all
```

Messages must be valid UTF-8 and at most 4096 encoded bytes. Loop signs the
message exactly, including file line endings. BIP-322 does not add a domain,
audience, nonce, network, or expiration, so the challenge issuer should include
all required replay protection in the message. A proof can include at most 1,000
deposits.

Loop does not provide a separate verification command. The returned signatures
use generic BIP-322 `ful` and `pof` formats and can be checked by compatible
BIP-322 tools.

## What the proof establishes

The full signature proves control of the client key required by the revealed
timeout leaf. That client key is also one member of the cooperative MuSig2
aggregate key, and the current construction has no other script leaves. The
client is therefore necessary for every spend path of the output, rather than
merely controlling one alternative branch. Verifying that stronger
Loop-specific statement requires the static-address parameters: recompute the
aggregate internal key and Taproot output from the client key, server key,
protocol version, and timeout leaf, then compare it with the signed address.

A proof of funds additionally commits to each listed outpoint, amount, output
script, and sequence. Loop includes confirmed outputs in any deposit FSM state
when lnd still reports them unspent. The response's confirmation count and FSM
state are snapshot metadata rather than cryptographic attestations.

An auditor must:

1. Use a compatible BIP-322 verifier to check the signature against the
   returned address and exact challenge.
2. Require `constrained=true`, `valid_at_time=0`, and the expected
   `valid_at_age`.
3. Parse every real proof input and independently query a trusted, current UTXO
   set for the same outpoint, amount, and P2TR output script.
4. Apply the reserves protocol's freshness, chain-tip, ownership aggregation,
   and replay rules.

The proof does not prove that the deposits remain unspent after it is returned,
that the selected set is complete, or that a unilateral real spend is already
CSV-mature.

## Privacy and authorization

Full and proof-of-funds signatures reveal the previously hidden timeout leaf,
client key, CSV delay, and the control block's internal key. This fingerprints
the output as a Loop-style static address and permanently links all proof
outpoints to each other and potentially to the identity in the challenge.
Share proofs only with parties that should retain that information.

The RPC requires the existing `swap:execute` and `loop:in` macaroon operations.
No new macaroon entity is introduced.
