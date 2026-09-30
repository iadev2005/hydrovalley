// Genera el código de Rust a partir de los contratos de `proto/` (raíz del
// repositorio). Usa el protoc de `protoc-bin-vendored`, así que no hace
// falta tener protoc instalado. El código generado queda en OUT_DIR y nunca
// se versiona.

use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto_root = proto_root();
    let world = proto_root.join("hydrovalley/world/v1/world.proto");
    let persistence = proto_root.join("hydrovalley/persistence/v1/persistence.proto");
    let catalog = proto_root.join("hydrovalley/persistence/v1/catalog.proto");

    let mut config = prost_build::Config::new();
    config.protoc_executable(protoc_bin_vendored::protoc_bin_path()?);

    tonic_prost_build::configure()
        .build_server(true)
        .build_client(true)
        .compile_with_config(
            config,
            &[world, persistence, catalog],
            std::slice::from_ref(&proto_root),
        )?;

    println!("cargo:rerun-if-changed={}", proto_root.display());
    Ok(())
}

// `proto/` está dos niveles por encima del crate, también dentro de la
// imagen de Docker, que copia el repositorio con la misma estructura.
fn proto_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../proto")
}
