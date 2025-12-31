/**
 * Neuro-OS Multi-Repository Configuration Manager
 * Manages authentication configs across neuro-relay, windows-api, and neuro-os
 */

const fs = require('fs');
const path = require('path');
// Simple YAML parser for basic configs (no external dependencies)
const yaml = {
    load: (content) => {
        // Basic YAML parsing - this handles simple key-value structures
        const lines = content.split('\n');
        const result = {};
        let currentSection = result;
        let indentStack = [result];
        
        for (const line of lines) {
            const trimmed = line.trim();
            if (!trimmed || trimmed.startsWith('#')) continue;
            
            const indent = line.length - line.trimLeft().length;
            const colonIndex = trimmed.indexOf(':');
            
            if (colonIndex > 0) {
                const key = trimmed.substring(0, colonIndex).trim();
                const value = trimmed.substring(colonIndex + 1).trim().replace(/^["']|["']$/g, '');
                
                // Handle indentation
                while (indentStack.length > Math.floor(indent / 2) + 1) {
                    indentStack.pop();
                }
                currentSection = indentStack[indentStack.length - 1];
                
                if (value) {
                    currentSection[key] = value;
                } else {
                    currentSection[key] = {};
                    indentStack.push(currentSection[key]);
                }
            }
        }
        return result;
    },
    
    dump: (obj, options = {}) => {
        const indent = '  ';
        
        function stringify(value, level = 0) {
            if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
                let result = '';
                for (const [key, val] of Object.entries(value)) {
                    result += indent.repeat(level) + key + ':';
                    if (typeof val === 'object' && val !== null && !Array.isArray(val)) {
                        result += '\n' + stringify(val, level + 1);
                    } else {
                        result += ' ' + val + '\n';
                    }
                }
                return result;
            } else {
                return String(value);
            }
        }
        
        return stringify(obj);
    }
};

class ConfigManager {
    constructor() {
        // Repository paths - go up to GitHub directory from neuro-os/src/dev/admin
        this.projectRoot = path.resolve(__dirname, '../../..');
        this.neuroRelayPath = path.resolve(this.projectRoot, '../neuro-relay');
        this.windowsApiPath = path.resolve(this.projectRoot, '../windows-api');
        this.neuroOsPath = this.projectRoot;
        
        // Config file paths
        this.relayAuthFile = path.join(this.neuroRelayPath, 'src', 'resources', 'authentication.yaml');
        this.windowsApiConfig = path.join(this.windowsApiPath, 'src', 'resources', 'gui', 'config', 'authentication.yaml');
        this.neuroOsConfig = path.join(this.neuroOsPath, 'src', 'resources', 'relay_auth.yaml');
    }

    /**
     * Load YAML configuration file
     */
    loadYamlConfig(filePath) {
        try {
            if (fs.existsSync(filePath)) {
                const content = fs.readFileSync(filePath, 'utf8');
                return yaml.load(content);
            } else {
                console.warn(`Config file not found: ${filePath}`);
                return {};
            }
        } catch (error) {
            console.error(`Failed to load config ${filePath}:`, error.message);
            return {};
        }
    }

    /**
     * Save YAML configuration file
     */
    saveYamlConfig(filePath, config) {
        try {
            // Ensure directory exists
            const dir = path.dirname(filePath);
            if (!fs.existsSync(dir)) {
                fs.mkdirSync(dir, { recursive: true });
            }
            
            const yamlContent = yaml.dump(config, { 
                defaultFlowStyle: false,
                lineWidth: 120,
                indent: 2 
            });
            fs.writeFileSync(filePath, yamlContent, 'utf8');
            console.log(`✅ Saved config: ${filePath}`);
            return true;
        } catch (error) {
            console.error(`❌ Failed to save config ${filePath}:`, error.message);
            return false;
        }
    }

    /**
     * Load neuro-relay authentication config
     */
    loadRelayConfig() {
        return this.loadYamlConfig(this.relayAuthFile);
    }

    /**
     * Save neuro-relay authentication config
     */
    saveRelayConfig(config) {
        return this.saveYamlConfig(this.relayAuthFile, config);
    }

    /**
     * Load windows-api authentication config
     */
    loadWindowsApiConfig() {
        return this.loadYamlConfig(this.windowsApiConfig);
    }

    /**
     * Save windows-api authentication config
     */
    saveWindowsApiConfig(config) {
        return this.saveYamlConfig(this.windowsApiConfig, config);
    }

    /**
     * Load neuro-os relay configuration
     */
    loadNeuroOsConfig() {
        const config = this.loadYamlConfig(this.neuroOsConfig);
        if (Object.keys(config).length === 0) {
            // Create default config if it doesn't exist
            return {
                relay_connection: {
                    host: "127.0.0.1",
                    port: 8765,
                    auth_token: "super-secret-token"
                },
                integrations: {
                    spotify: { enabled: true },
                    discord: { enabled: true },
                    twitch: { enabled: true }
                },
                logging: {
                    level: "INFO",
                    file: "logs/neuro_os.log"
                }
            };
        }
        return config;
    }

    /**
     * Save neuro-os relay configuration
     */
    saveNeuroOsConfig(config) {
        return this.saveYamlConfig(this.neuroOsConfig, config);
    }

    /**
     * Synchronize authentication tokens across all configs
     */
    syncAuthTokens(newToken = "super-secret-token") {
        console.log(`🔄 Synchronizing auth token: ${newToken}`);
        let success = true;

        // Update relay config
        const relayConfig = this.loadRelayConfig();
        if (relayConfig && Object.keys(relayConfig).length > 0) {
            if (relayConfig.intermediary) relayConfig.intermediary.auth_token = newToken;
            if (relayConfig["dependency-authentication"] && relayConfig["dependency-authentication"]["neuro-os"]) {
                relayConfig["dependency-authentication"]["neuro-os"].auth_token = newToken;
            }
            success &= this.saveRelayConfig(relayConfig);
        }

        // Update windows API config
        let windowsConfig = this.loadWindowsApiConfig();
        if (Object.keys(windowsConfig).length === 0) {
            // Create default windows API config
            windowsConfig = {
                neuro_relay: {
                    auth_token: newToken,
                    host: "127.0.0.1",
                    port: 8765
                },
                integrations: {
                    enabled: true
                }
            };
        } else {
            if (!windowsConfig.neuro_relay) windowsConfig.neuro_relay = {};
            windowsConfig.neuro_relay.auth_token = newToken;
        }
        success &= this.saveWindowsApiConfig(windowsConfig);

        // Update neuro-os config
        const neuroOsConfig = this.loadNeuroOsConfig();
        if (neuroOsConfig.relay_connection) {
            neuroOsConfig.relay_connection.auth_token = newToken;
        }
        success &= this.saveNeuroOsConfig(neuroOsConfig);

        if (success) {
            console.log("✅ Successfully synchronized auth tokens across all configs");
        } else {
            console.error("❌ Failed to synchronize some auth tokens");
        }

        return success;
    }

    /**
     * Get status of all configuration files
     */
    getSystemStatus() {
        return {
            neuro_relay: {
                path: this.relayAuthFile,
                exists: fs.existsSync(this.relayAuthFile),
                config: this.loadRelayConfig()
            },
            windows_api: {
                path: this.windowsApiConfig,
                exists: fs.existsSync(this.windowsApiConfig),
                config: this.loadWindowsApiConfig()
            },
            neuro_os: {
                path: this.neuroOsConfig,
                exists: fs.existsSync(this.neuroOsConfig),
                config: this.loadNeuroOsConfig()
            }
        };
    }

    /**
     * Create backup of all configuration files
     */
    backupConfigs() {
        const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
        const backupDir = path.join(this.neuroOsPath, 'backups', `configs_${timestamp}`);
        
        try {
            if (!fs.existsSync(backupDir)) {
                fs.mkdirSync(backupDir, { recursive: true });
            }

            // Backup relay config
            if (fs.existsSync(this.relayAuthFile)) {
                const backupRelay = path.join(backupDir, 'relay_authentication.yaml');
                fs.copyFileSync(this.relayAuthFile, backupRelay);
            }

            // Backup windows API config
            if (fs.existsSync(this.windowsApiConfig)) {
                const backupWindows = path.join(backupDir, 'windows_api_authentication.yaml');
                fs.copyFileSync(this.windowsApiConfig, backupWindows);
            }

            // Backup neuro-os config
            if (fs.existsSync(this.neuroOsConfig)) {
                const backupNeuro = path.join(backupDir, 'neuro_os_relay_auth.yaml');
                fs.copyFileSync(this.neuroOsConfig, backupNeuro);
            }

            console.log(`✅ Backed up configs to: ${backupDir}`);
            return true;
        } catch (error) {
            console.error(`❌ Failed to backup configs:`, error.message);
            return false;
        }
    }

    /**
     * Initialize all configs with default values
     */
    initializeConfigs() {
        console.log("🚀 Initializing default configurations...");
        
        // Ensure neuro-os config exists
        const neuroOsConfig = this.loadNeuroOsConfig();
        this.saveNeuroOsConfig(neuroOsConfig);
        
        // Sync tokens to ensure consistency
        this.syncAuthTokens();
        
        console.log("✅ Configuration initialization complete");
    }
}

/**
 * Interactive CLI for managing configs
 */
async function runInteractiveCLI() {
    const readline = require('readline');
    const rl = readline.createInterface({
        input: process.stdin,
        output: process.stdout
    });

    const question = (prompt) => new Promise(resolve => rl.question(prompt, resolve));
    const configManager = new ConfigManager();

    console.log("=== Neuro-OS Configuration Manager ===");
    console.log("Managing authentication across neuro-relay, windows-api, and neuro-os");
    console.log();

    while (true) {
        console.log("Options:");
        console.log("1. View system status");
        console.log("2. Sync authentication tokens");
        console.log("3. Backup all configs");
        console.log("4. Update auth token");
        console.log("5. Initialize configs");
        console.log("6. Exit");
        console.log();

        const choice = await question("Select option (1-6): ");

        switch (choice.trim()) {
            case "1":
                console.log("\n=== System Status ===");
                const status = configManager.getSystemStatus();
                for (const [system, info] of Object.entries(status)) {
                    console.log(`\n${system.toUpperCase()}:`);
                    console.log(`  Path: ${info.path}`);
                    console.log(`  Exists: ${info.exists}`);
                    if (info.config && Object.keys(info.config).length > 0) {
                        console.log(`  Config keys: ${Object.keys(info.config).join(', ')}`);
                    }
                }
                break;

            case "2":
                console.log("\n=== Syncing Authentication Tokens ===");
                configManager.syncAuthTokens();
                break;

            case "3":
                console.log("\n=== Creating Backup ===");
                configManager.backupConfigs();
                break;

            case "4":
                const newToken = await question("Enter new auth token (or press Enter for default): ");
                const token = newToken.trim() || "super-secret-token";
                console.log(`\n=== Updating to token: ${token} ===`);
                configManager.syncAuthTokens(token);
                break;

            case "5":
                console.log("\n=== Initializing Configs ===");
                configManager.initializeConfigs();
                break;

            case "6":
                console.log("Goodbye!");
                rl.close();
                return;

            default:
                console.log("Invalid option, please try again.");
        }

        console.log("\n" + "=".repeat(50) + "\n");
    }
}

// Export for use as module
module.exports = { ConfigManager };

// Run interactively if called directly
if (require.main === module) {
    runInteractiveCLI().catch(console.error);
}