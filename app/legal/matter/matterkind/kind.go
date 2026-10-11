// Package matterkind holds the matter module's registry subject kind. It imports nothing,
// so modules that the matter package itself depends on (roles, through conflict) can name
// the kind without an import cycle and without spelling a matter table name themselves
// (ARC-I-041).
package matterkind

// Subject is the registry subject kind of a matter; its subject id is the matter id.
const Subject = "matter.matter"

// GatePartyConflict is G2 on a matter (matter.party_conflict): a party joins the matter only
// once a lawyer clears the conflict check proposed for it. The agent tools name it here so
// they need not import the matter package.
const GatePartyConflict = "matter.party_conflict"
