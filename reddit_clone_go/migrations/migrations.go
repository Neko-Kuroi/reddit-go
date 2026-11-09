// Package migrations holds the SQL migration files, embedded into the
// binary so a single file is all that needs to be deployed.
//
// Files are named NNNN_description.up.sql / NNNN_description.down.sql and
// correspond one-to-one with the Python version's Alembic chain
// (revisions 0001 to 0009).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
