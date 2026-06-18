package build

// Worker integration is intentionally kept out of HTTP handlers. The first
// implementation records platform facts; Jenkins execution can plug into this
// module through a queue consumer without changing route registration.
