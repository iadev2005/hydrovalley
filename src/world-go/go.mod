module github.com/iadev2005/hydrovalley/src/world-go

go 1.26

// El código generado desde proto/ (gen/go) lo usan el codec y el servidor
// gRPC (T6 y T8); el dominio no depende de él.
replace github.com/iadev2005/hydrovalley/gen/go => ../../gen/go
