# src/admin/dashboard.py
import os
import yaml
import json
from pathlib import Path
from flask import Flask, render_template, request, jsonify, redirect, url_for, flash
from datetime import datetime
import shutil
import logging

app = Flask(__name__)
app.secret_key = 'neuro-os-admin-dashboard-secret-key'

# Setup logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

class ConfigManager:
    def __init__(self):
        self.base_path = Path(__file__).parents[2].resolve()
        self.configs = {
            'neuro_os': {
                'path': self.base_path / 'src' / 'resources' / 'relay_auth.yaml',
                'name': 'Neuro-OS Relay Authentication'
            },
            'neuro_relay': {
                'path': self.base_path.parent / 'neuro-relay' / 'src' / 'resources' / 'authentication.yaml',
                'name': 'Neuro-Relay Authentication'
            },
            'windows_api': {
                'path': self.base_path.parent / 'windows-api' / 'src' / 'resources' / 'gui' / 'config' / 'authentication.yaml',
                'name': 'Windows-API Authentication'
            }
        }
        
        # Create backup directory
        self.backup_dir = self.base_path / 'backups'
        self.backup_dir.mkdir(exist_ok=True)
    
    def load_config(self, config_name):
        """Load a configuration file"""
        try:
            config_info = self.configs[config_name]
            with open(config_info['path'], 'r') as f:
                return yaml.safe_load(f), None
        except FileNotFoundError:
            return None, f"Config file not found: {config_info['path']}"
        except yaml.YAMLError as e:
            return None, f"YAML parsing error: {str(e)}"
        except Exception as e:
            return None, f"Error loading config: {str(e)}"
    
    def save_config(self, config_name, data):
        """Save a configuration file with backup"""
        try:
            config_info = self.configs[config_name]
            
            # Create backup before saving
            self._create_backup(config_name)
            
            # Save new config
            with open(config_info['path'], 'w') as f:
                yaml.dump(data, f, default_flow_style=False, sort_keys=False)
            
            logger.info(f"Saved config: {config_name}")
            return True, None
        except Exception as e:
            error_msg = f"Error saving config {config_name}: {str(e)}"
            logger.error(error_msg)
            return False, error_msg
    
    def _create_backup(self, config_name):
        """Create a backup of the current config"""
        try:
            config_info = self.configs[config_name]
            if config_info['path'].exists():
                timestamp = datetime.now().strftime('%Y-%m-%d_%H-%M-%S')
                backup_filename = f"{config_name}_{timestamp}.yaml"
                backup_path = self.backup_dir / backup_filename
                shutil.copy2(config_info['path'], backup_path)
                logger.info(f"Created backup: {backup_path}")
        except Exception as e:
            logger.error(f"Failed to create backup for {config_name}: {str(e)}")
    
    def get_all_configs(self):
        """Load all configuration files"""
        configs = {}
        for name, info in self.configs.items():
            data, error = self.load_config(name)
            configs[name] = {
                'data': data,
                'error': error,
                'name': info['name'],
                'path': str(info['path']),
                'exists': info['path'].exists()
            }
        return configs
    
    def validate_config(self, config_name, data):
        """Validate configuration data"""
        validation_rules = {
            'neuro_os': self._validate_neuro_os,
            'neuro_relay': self._validate_neuro_relay,
            'windows_api': self._validate_windows_api
        }
        
        validator = validation_rules.get(config_name)
        if validator:
            return validator(data)
        return True, None
    
    def _validate_neuro_os(self, data):
        """Validate neuro-os config"""
        required_fields = ['relay_connection']
        for field in required_fields:
            if field not in data:
                return False, f"Missing required field: {field}"
        
        relay_conn = data['relay_connection']
        if not isinstance(relay_conn.get('port'), int):
            return False, "relay_connection.port must be an integer"
        
        return True, None
    
    def _validate_neuro_relay(self, data):
        """Validate neuro-relay config"""
        required_sections = ['intermediary', 'nakurity-backend', 'nakurity-client']
        for section in required_sections:
            if section not in data:
                return False, f"Missing required section: {section}"
        
        # Validate port numbers
        for section in required_sections:
            if 'port' in data[section] and not isinstance(data[section]['port'], int):
                return False, f"{section}.port must be an integer"
        
        return True, None
    
    def _validate_windows_api(self, data):
        """Validate windows-api config"""
        if 'port' in data and not isinstance(data['port'], int):
            return False, "port must be an integer"
        
        if 'pause' in data and not isinstance(data['pause'], (int, float)):
            return False, "pause must be a number"
        
        return True, None

config_manager = ConfigManager()

@app.route('/')
def dashboard():
    """Main dashboard page"""
    configs = config_manager.get_all_configs()
    return render_template('dashboard.html', configs=configs)

@app.route('/config/<config_name>')
def view_config(config_name):
    """View and edit a specific configuration"""
    if config_name not in config_manager.configs:
        flash(f"Unknown configuration: {config_name}", 'error')
        return redirect(url_for('dashboard'))
    
    data, error = config_manager.load_config(config_name)
    config_info = config_manager.configs[config_name]
    
    return render_template('config_editor.html', 
                         config_name=config_name,
                         config_data=data,
                         config_info=config_info,
                         error=error)

@app.route('/api/config/<config_name>', methods=['GET'])
def get_config_api(config_name):
    """API endpoint to get configuration data"""
    data, error = config_manager.load_config(config_name)
    if error:
        return jsonify({'error': error}), 400
    return jsonify(data)

@app.route('/api/config/<config_name>', methods=['POST'])
def save_config_api(config_name):
    """API endpoint to save configuration data"""
    try:
        data = request.get_json()
        
        # Validate the configuration
        is_valid, error = config_manager.validate_config(config_name, data)
        if not is_valid:
            return jsonify({'error': f"Validation failed: {error}"}), 400
        
        # Save the configuration
        success, error = config_manager.save_config(config_name, data)
        if not success:
            return jsonify({'error': error}), 500
        
        return jsonify({'success': True, 'message': f'Configuration {config_name} saved successfully'})
    
    except Exception as e:
        logger.error(f"Error in save_config_api: {str(e)}")
        return jsonify({'error': str(e)}), 500

@app.route('/save_config/<config_name>', methods=['POST'])
def save_config_form(config_name):
    """Handle form submission for configuration saving"""
    try:
        # Get the YAML data from the form
        yaml_content = request.form.get('yaml_content')
        
        # Parse the YAML
        try:
            data = yaml.safe_load(yaml_content)
        except yaml.YAMLError as e:
            flash(f"YAML parsing error: {str(e)}", 'error')
            return redirect(url_for('view_config', config_name=config_name))
        
        # Validate the configuration
        is_valid, error = config_manager.validate_config(config_name, data)
        if not is_valid:
            flash(f"Validation failed: {error}", 'error')
            return redirect(url_for('view_config', config_name=config_name))
        
        # Save the configuration
        success, error = config_manager.save_config(config_name, data)
        if success:
            flash(f'Configuration {config_name} saved successfully!', 'success')
        else:
            flash(f'Failed to save configuration: {error}', 'error')
        
        return redirect(url_for('view_config', config_name=config_name))
    
    except Exception as e:
        logger.error(f"Error in save_config_form: {str(e)}")
        flash(f'Error: {str(e)}', 'error')
        return redirect(url_for('view_config', config_name=config_name))

@app.route('/api/sync_tokens', methods=['POST'])
def sync_tokens():
    """Sync authentication tokens across all configurations"""
    try:
        data = request.get_json()
        token = data.get('token')
        
        if not token:
            return jsonify({'error': 'Token is required'}), 400
        
        # Update tokens in all configurations
        results = {}
        
        # Update neuro-os
        neuro_os_data, error = config_manager.load_config('neuro_os')
        if neuro_os_data and not error:
            neuro_os_data['relay_connection']['auth_token'] = token
            success, error = config_manager.save_config('neuro_os', neuro_os_data)
            results['neuro_os'] = {'success': success, 'error': error}
        else:
            results['neuro_os'] = {'success': False, 'error': error}
        
        # Update neuro-relay
        neuro_relay_data, error = config_manager.load_config('neuro_relay')
        if neuro_relay_data and not error:
            neuro_relay_data['intermediary']['auth_token'] = token
            neuro_relay_data['dependency-authentication']['neuro-os']['auth_token'] = token
            success, error = config_manager.save_config('neuro_relay', neuro_relay_data)
            results['neuro_relay'] = {'success': success, 'error': error}
        else:
            results['neuro_relay'] = {'success': False, 'error': error}
        
        # Update windows-api
        windows_api_data, error = config_manager.load_config('windows_api')
        if windows_api_data and not error:
            windows_api_data['neuro_relay']['auth_token'] = token
            success, error = config_manager.save_config('windows_api', windows_api_data)
            results['windows_api'] = {'success': success, 'error': error}
        else:
            results['windows_api'] = {'success': False, 'error': error}
        
        return jsonify({
            'success': all(r.get('success', False) for r in results.values()),
            'results': results,
            'message': 'Token synchronization completed'
        })
    
    except Exception as e:
        logger.error(f"Error in sync_tokens: {str(e)}")
        return jsonify({'error': str(e)}), 500

@app.route('/api/backups')
def list_backups():
    """List all available backup files"""
    try:
        backups = []
        if config_manager.backup_dir.exists():
            for backup_file in config_manager.backup_dir.glob('*.yaml'):
                stat = backup_file.stat()
                backups.append({
                    'filename': backup_file.name,
                    'path': str(backup_file),
                    'size': stat.st_size,
                    'created': datetime.fromtimestamp(stat.st_ctime).isoformat(),
                    'modified': datetime.fromtimestamp(stat.st_mtime).isoformat()
                })
        
        # Sort by creation time, newest first
        backups.sort(key=lambda x: x['created'], reverse=True)
        return jsonify(backups)
    
    except Exception as e:
        logger.error(f"Error listing backups: {str(e)}")
        return jsonify({'error': str(e)}), 500

if __name__ == '__main__':
    # Create templates directory and basic templates if they don't exist
    templates_dir = Path(__file__).parent / 'templates'
    templates_dir.mkdir(exist_ok=True)
    
    # Run the Flask app
    app.run(debug=True, host='127.0.0.1', port=5000)