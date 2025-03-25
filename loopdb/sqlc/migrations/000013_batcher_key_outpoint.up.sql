-- We want to make column swap_hash non-unique and to use the outpoint as a key.
-- We can't make a column non-unique or remove it in sqlite, so work around.
-- See https://stackoverflow.com/a/42013422

-- We also made outpoint a single point replacing columns outpoint_txid and
-- outpoint_index.

-- sweeps stores the individual sweeps that are part of a batch.
CREATE TABLE sweeps2 (
        -- id is the autoincrementing primary key.
        id INTEGER PRIMARY KEY,

        -- swap_hash is the hash of the swap that is being swept.
        swap_hash BLOB NOT NULL,

        -- batch_id is the id of the batch this swap is part of.
        batch_id INTEGER NOT NULL,

        -- outpoint is the UTXO id of the output being swept ("txid:index").
        outpoint BLOB NOT NULL UNIQUE,

        -- amt is the amount of the output being swept.
        amt BIGINT NOT NULL,

        -- completed indicates whether the sweep has been completed.
        completed BOOLEAN NOT NULL DEFAULT FALSE,

        -- Foreign key constraint to ensure that we reference an existing batch
        -- id.
        FOREIGN KEY (batch_id) REFERENCES sweep_batches(id),

        -- Foreign key constraint to ensure that swap_hash references an
        -- existing swap.
        FOREIGN KEY (swap_hash) REFERENCES swaps(swap_hash)
);

-- Copy all the data from sweeps to sweeps2.
INSERT INTO sweeps2 (
        id, swap_hash, batch_id, outpoint, amt, completed
) SELECT
        id, swap_hash, batch_id, outpoint_txid || ':' || cast(cast(outpoint_index AS TEXT) AS BLOB), amt,
        completed
FROM sweeps;

-- Rename tables.
ALTER TABLE sweeps RENAME TO sweeps_old;
ALTER TABLE sweeps2 RENAME TO sweeps;
