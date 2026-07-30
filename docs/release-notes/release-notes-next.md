# Loop Client Release Notes

#### New Features

* Add `loop static bip322 ful` and `pof` commands to sign messages with a static
  address and prove ownership of confirmed funds held there. Before returning
  a result, loopd validates the persisted client key, checks every lnd
  signature, and rechecks the selected UTXOs.

#### Breaking Changes

#### Bug Fixes

#### Maintenance

#### Contributors (Alphabetical Order)
