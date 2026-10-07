// Packages under x/ are ones Miren's own services import from outside this
// repository. Every requirement listed here is paid by every importer, even one
// that uses a single package, so keep it to small, stable libraries.
module miren.dev/runtime/x

go 1.24.0

require (
	github.com/go-jose/go-jose/v4 v4.1.4
	github.com/golang-jwt/jwt/v5 v5.3.1
)
