//! Proceso del simulador: lee la configuración, abre el puerto y sirve
//! hasta recibir SIGINT o SIGTERM.

use std::io::IsTerminal;
use std::process::ExitCode;

use tokio::net::TcpListener;
use tracing_subscriber::EnvFilter;

use world::config::Config;
use world::server::{Settings, serve};

#[tokio::main]
async fn main() -> ExitCode {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_env("LOG_LEVEL").unwrap_or_else(|_| EnvFilter::new("info")),
        )
        // Sin códigos de color cuando la salida no es una terminal (docker logs).
        .with_ansi(std::io::stdout().is_terminal())
        .init();

    match run().await {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            tracing::error!(%error, "el simulador se detuvo");
            ExitCode::FAILURE
        }
    }
}

async fn run() -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let config = Config::from_env()?;
    let listener = TcpListener::bind(config.listen).await?;
    tracing::info!(
        listen = %config.listen,
        persistence = %config.persistence_addr,
        "WorldService escuchando"
    );
    serve(
        Settings::new(config.persistence_addr),
        listener,
        shutdown_signal(),
    )
    .await
}

async fn shutdown_signal() {
    let interrupt = async {
        if let Err(error) = tokio::signal::ctrl_c().await {
            tracing::error!(%error, "no se pudo escuchar SIGINT");
            std::future::pending::<()>().await;
        }
    };
    #[cfg(unix)]
    let terminate = async {
        match tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate()) {
            Ok(mut signal) => {
                signal.recv().await;
            }
            Err(error) => {
                tracing::error!(%error, "no se pudo escuchar SIGTERM");
                std::future::pending::<()>().await;
            }
        }
    };
    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        () = interrupt => {},
        () = terminate => {},
    }
    tracing::info!("señal de parada recibida");
}
