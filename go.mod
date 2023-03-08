module github.com/icon-project/btp2-eth2

go 1.13

replace github.com/attestantio/go-eth2-client => github.com/eunsoo-icon/go-eth2-client v0.6.1

require (
	github.com/attestantio/go-eth2-client v0.0.0-00010101000000-000000000000
	github.com/btcsuite/btcd v0.23.3 // indirect
	github.com/btcsuite/btcd/btcec/v2 v2.3.2 // indirect
	github.com/btcsuite/btcd/chaincfg/chainhash v1.0.2 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.1.0 // indirect
	github.com/ethereum/go-ethereum v1.11.3
	github.com/ferranbt/fastssz v0.1.3
	github.com/icon-project/btp2 v0.0.0-20230308003505-49ecd73e8040
	github.com/rs/zerolog v1.29.0
	github.com/spf13/cobra v1.6.1
	github.com/stretchr/testify v1.8.1
	golang.org/x/crypto v0.7.0 // indirect
)
