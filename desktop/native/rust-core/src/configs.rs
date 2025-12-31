use serde::Deserialize;

#[derive(Debug, Deserialize)]
pub struct NeuroIntegrationConfig {
    pub neuro_backend_url: String,
    pub plugin_server_url: String,
    pub admin_dashboard_url: String,
    pub internal_thinking_viewer_url: String,
}

#[derive(Debug, Deserialize)]
pub struct PackageInformation {
    pub name: String,
    pub version: String,
    pub description: String,
    pub build: String
}

use config::{Config, File, FileFormat};
use paths::get_config_path;

pub fn get_integration_config() -> Result<NeuroIntegrationConfig, config::ConfigError> {
    let settings = Config::builder()
        // Add the configuration file source
        .add_source(File::new(get_config_path().join("integration-config.yaml"), FileFormat::Yaml))
        // Add other sources like environment variables if needed
        .build()?;

    // Try to deserialize the configuration into your struct
    settings.try_deserialize::<NeuroIntegrationConfig>()
}