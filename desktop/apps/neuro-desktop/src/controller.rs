use anyhow::Result;
use pyo3::prelude::*;
use pyo3::types::{PyTuple};

use rust_core::paths::get_python_packages_path;

pub struct Controller {
    monitor: Py<PyAny>,
    mouse: Py<PyAny>,
    keyboard: Py<PyAny>,
    parser: Py<PyAny>,
}

impl Controller {
    pub fn initialize_drivers() -> Result<Self> {
        Python::with_gil(|py| -> PyResult<Self> {
            // -------------------------------------------------
            // Configure Python path
            // -------------------------------------------------
            let sys = py.import_bound("sys")?;
            let path = sys.getattr("path")?;
            let path = path.downcast::<pyo3::types::PyList>()?;

            path.insert(0, get_python_packages_path().to_str().unwrap())?;

            // -------------------------------------------------
            // Import lib module (the control driver's entrypoint)
            // -------------------------------------------------
            let lib = py.import_bound("controller.lib")?;

            // -------------------------------------------------
            // Call factory function
            // -------------------------------------------------
            let result = lib.getattr("initialize_driver")?.call0()?;
            let tuple = result.downcast::<PyTuple>()?;

            Ok(Self {
                monitor: tuple.get_item(0)?.into(),
                mouse: tuple.get_item(1)?.into(),
                keyboard: tuple.get_item(2)?.into(),
                parser: tuple.get_item(3)?.into(),
            })
        })
        .map_err(Into::into)
    }

    // =====================================================
    // Script execution (preferred API)
    // =====================================================

    pub fn run_script(&self, script: &str) -> Result<()> {
        Python::with_gil(|py| {
            self.parser
                .bind(py)
                .getattr("parse")?
                .call1((script,))?;

            self.keyboard.bind(py).getattr("execute")?.call0()?;
            self.mouse.bind(py).getattr("execute")?.call0()?;
            Ok::<(), PyErr>(())
        })
        .map_err(Into::into)
    }

    // Used to execute manual low-level calls (required when calling low-level APIs)
    pub fn execute_instructions(&self) -> Result<()> {
        Python::with_gil(|py| {
            self.keyboard.bind(py).getattr("execute")?.call0()?;
            self.mouse.bind(py).getattr("execute")?.call0()?;
            Ok::<(), PyErr>(())
        })
        .map_err(Into::into)
    }

    // =====================================================
    // Low-level direct calls (optional)
    // =====================================================

    pub fn mouse_move(&self, x: i32, y: i32) -> Result<()> {
        Python::with_gil(|py| {
            self.mouse
                .bind(py)
                .getattr("queue_move")?
                .call1((x, y))?;
            Ok::<(), PyErr>(())
        })
        .map_err(Into::into)
    }

    pub fn mouse_click(&self, x: i32, y: i32, button: &str) -> Result<()> {
        Python::with_gil(|py| {
            self.mouse
                .bind(py)
                .getattr("queue_click")?
                .call1((x, y, button))?;
            Ok::<(), PyErr>(())
        })
        .map_err(Into::into)
    }

    pub fn type_text(&self, text: &str) -> Result<()> {
        Python::with_gil(|py| {
            self.keyboard
                .bind(py)
                .getattr("type")?
                .call1((text,))?;
            Ok::<(), PyErr>(())
        })
        .map_err(Into::into)
    }

    pub fn clear_action_queue(&self) -> Result<()> {
        Python::with_gil(|py| {
            self.mouse.bind(py).getattr("clear")?.call0()?;
            self.keyboard.bind(py).getattr("clear")?.call0()?;
            Ok::<(), PyErr>(())
        })
        .map_err(Into::into)
    }

    // =====================================================
    // Telemetry access
    // =====================================================

    pub fn action_history(&self) -> Result<String> {
        Python::with_gil(|py| {
            let history = self
                .monitor
                .bind(py)
                .getattr("get_action_history")?
                .call0()?;

            Ok::<_, PyErr>(history.str()?.to_string())
        })
        .map_err(Into::into)
    }

    /// Expose the DesktopMonitor Python object  
    pub fn get_monitor(&self) -> &Py<PyAny> {
        &self.monitor
    }

    pub fn shutdown(&self) -> Result<()> {
        Python::with_gil(|py| {
            self.monitor.bind(py).getattr("shutdown")?.call0()?;
            Ok::<(), PyErr>(())
        })
        .map_err(Into::into)
    }
}

// // Example usage
//
// let controller = Controller::initialize_drivers()?;  
//
// // High-level script
// controller.run_script(r#"
// TYPE "git status"
// ENTER
// WAIT 0.3
// TYPE "git commit -m 'fix'"
// ENTER
// "#)?;
//
// // Inspect what happened
// println!("{}", controller.action_history()?);
//
// // Cleanup
// controller.shutdown()?;
