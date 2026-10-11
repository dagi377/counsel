// Package matterkind holds the matter module's registry subject kind. It imports nothing,
// so modules that the matter package itself depends on (roles, through conflict) can name
// the kind without an import cycle and without spelling a matter table name themselves
// (ARC-I-041).
package matterkind

// Subject is the registry subject kind of a matter; its subject id is the matter id.
const Subject = "matter.matter"
