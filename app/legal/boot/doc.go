// Package boot is the Counsel app's composition root. App returns the kernel boot.App that
// defines the legal flags, registers every legal module and the services built from kernel
// dependencies, and starts the app's seed jobs.
//
// Only cmd/counseld and cmd/counsel-worker import this package. It builds services through
// module constructors only: it holds no SQL and imports no store package.
package boot
