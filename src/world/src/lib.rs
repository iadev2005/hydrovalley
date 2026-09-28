//! Simulador del valle de hydrovalley (escritor único del mundo).
//!
//! - [`domain`]: reglas puras del valle, sin gRPC.
//! - [`codec`]: traducción entre el contrato y el dominio.
//! - [`store`]: puerto y adaptador gRPC de las instantáneas.
//! - [`simulator`]: servicio de aplicación (arranque, tick e instantáneas).
//! - [`grpc`], [`server`] y [`config`]: la frontera de red.
//! - [`proto`]: código generado desde `proto/` en `build.rs`.

pub mod codec;
pub mod config;
pub mod domain;
pub mod grpc;
pub mod server;
pub mod simulator;
pub mod store;

/// Código generado a partir de los contratos de `proto/`.
pub mod proto {
    pub mod world {
        pub mod v1 {
            tonic::include_proto!("hydrovalley.world.v1");
        }
    }
    pub mod persistence {
        pub mod v1 {
            tonic::include_proto!("hydrovalley.persistence.v1");
        }
    }
}
