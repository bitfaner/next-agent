use std::sync::Mutex;

use tauri::{AppHandle, Manager, State};
use tauri_plugin_shell::{process::CommandEvent, ShellExt};

const BACKEND_URL: &str = "http://127.0.0.1:34121";

struct BackendState {
    child: Mutex<Option<tauri_plugin_shell::process::CommandChild>>,
}

#[tauri::command]
fn backend_base_url() -> &'static str {
    BACKEND_URL
}

fn spawn_backend(app: &tauri::AppHandle) {
    let state = app.state::<BackendState>();
    let mut guard = state.child.lock().expect("backend state poisoned");
    if guard.is_some() {
        return;
    }

    match app.shell().sidecar("nextx-agent") {
        Ok(command) => match command.args(["--addr", "127.0.0.1:34121"]).spawn() {
            Ok((mut rx, child)) => {
                tauri::async_runtime::spawn(async move {
                    while let Some(event) = rx.recv().await {
                        match event {
                            CommandEvent::Stdout(line) => {
                                println!("backend: {}", String::from_utf8_lossy(&line));
                            }
                            CommandEvent::Stderr(line) => {
                                eprintln!("backend: {}", String::from_utf8_lossy(&line));
                            }
                            _ => {}
                        }
                    }
                });
                *guard = Some(child);
            }
            Err(err) => eprintln!("failed to spawn backend sidecar: {err}"),
        },
        Err(err) => eprintln!("failed to create backend sidecar command: {err}"),
    }
}

fn stop_backend(app: &AppHandle) {
    let state = app.state::<BackendState>();
    let child = state.child.lock().expect("backend state poisoned").take();
    if let Some(child) = child {
        let _ = child.kill();
    }
}

pub fn run() {
    let app = tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_shell::init())
        .manage(BackendState {
            child: Mutex::new(None),
        })
        .setup(|app| {
            spawn_backend(app.handle());
            if let Some(window) = app.get_webview_window("main") {
                if let Err(err) = window.show() {
                    eprintln!("failed to show main window: {err}");
                }
                if let Err(err) = window.set_focus() {
                    eprintln!("failed to focus main window: {err}");
                }
            } else {
                eprintln!("main window was not created");
            }
            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { .. } = event {
                let state: State<BackendState> = window.state();
                let child = state.child.lock().expect("backend state poisoned").take();
                if let Some(child) = child {
                    let _ = child.kill();
                }
            }
        })
        .invoke_handler(tauri::generate_handler![backend_base_url])
        .build(tauri::generate_context!())
        .expect("error while building tauri application");

    app.run(|app_handle, event| {
        if matches!(
            event,
            tauri::RunEvent::Exit | tauri::RunEvent::ExitRequested { .. }
        ) {
            stop_backend(app_handle);
        }
    });
}
