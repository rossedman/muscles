package analysis

import "github.com/rossedman/muscles/internal/pkgdb"

// pkgdbMax is one byte over the largest package database the indexer reads.
func pkgdbMax() int64 { return pkgdb.MaxDB + 1 }
