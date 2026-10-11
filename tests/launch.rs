//! Launching applications for real (src/launch): the fixture application
//! tests/fixtures/apps/bin/anoint records how it was started; a fake session
//! bus (tests/support/fake_bus.rs) stands in for systemd's user manager and
//! for D-Bus activation. Nothing here reaches the real session bus.

mod support;

use std::collections::HashMap;
use std::ffi::OsString;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use logis::launch::wire::Value;
use logis::launch::{LaunchError, Plan, Session, Via, launch};
use support::ScratchDir;
use support::fake_bus::{Answer, FakeBus};

fn bin() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/apps/bin")
}

/// Where a test's launch records itself, and the file that holds it alive.
struct Rite {
    dir: ScratchDir,
}

impl Rite {
    fn new() -> Self {
        let rite = Self {
            dir: ScratchDir::new(),
        };
        std::fs::write(rite.hold(), b"").unwrap();
        rite
    }

    fn record(&self) -> PathBuf {
        self.dir.path().join("record")
    }

    fn hold(&self) -> PathBuf {
        self.dir.path().join("hold")
    }

    /// The environment of a launch, `marker` telling its source apart.
    fn env(&self, marker: &str) -> Vec<(OsString, OsString)> {
        let path = format!("{}:/usr/bin:/bin", bin().display());
        [
            ("PATH", OsString::from(path)),
            ("HOME", self.dir.path().into()),
            ("ANOINT_RECORD", self.record().into()),
            ("ANOINT_HOLD", self.hold().into()),
            ("ANOINT_MARKER", marker.into()),
            ("XDG_ACTIVATION_TOKEN", "stale-token".into()),
        ]
        .into_iter()
        .map(|(k, v)| (OsString::from(k), v))
        .collect()
    }

    /// What the application recorded: each key's values in order.
    fn heard(&self) -> HashMap<String, Vec<String>> {
        let deadline = Instant::now() + Duration::from_secs(5);
        while !self.record().exists() {
            assert!(Instant::now() < deadline, "the application never ran");
            std::thread::sleep(Duration::from_millis(5));
        }
        let mut heard: HashMap<String, Vec<String>> = HashMap::new();
        for line in std::fs::read_to_string(self.record()).unwrap().lines() {
            let (key, value) = line.split_once(' ').unwrap_or((line, ""));
            heard.entry(key.into()).or_default().push(value.into());
        }
        heard
    }

    /// Lets the application end, and waits until it is gone (reaped).
    fn release(&self, pid: u32) {
        let _ = std::fs::remove_file(self.hold());
        let deadline = Instant::now() + Duration::from_secs(5);
        while Path::new(&format!("/proc/{pid}")).exists() {
            assert!(
                Instant::now() < deadline,
                "the application lingers (a zombie?)"
            );
            std::thread::sleep(Duration::from_millis(5));
        }
    }
}

fn one(heard: &HashMap<String, Vec<String>>, key: &str) -> String {
    heard[key][0].clone()
}

fn anoint(args: &[&str]) -> Plan {
    let mut argv = vec!["anoint".to_owned()];
    argv.extend(args.iter().map(|a| (*a).to_owned()));
    Plan {
        app_id: "forge-anoint".into(),
        name: "Anoint".into(),
        argv,
        ..Plan::default()
    }
}

#[test]
fn without_a_bus_the_app_runs_detached_in_the_callers_environment() {
    let rite = Rite::new();
    let session = Session {
        bus: Some("unix:path=/nonexistent/logis-test-bus".into()),
        env: rite.env("caller"),
    };
    let launched = launch(&anoint(&["--vessel", "two words"]), Some("tok-1"), &session).unwrap();
    assert_eq!(launched.via, Via::Plain);
    let pid = launched.pid.unwrap();
    let heard = rite.heard();
    assert_eq!(one(&heard, "pid"), pid.to_string());
    assert_eq!(heard["arg"], ["--vessel", "two words"]);
    assert_eq!(one(&heard, "marker"), "caller");
    assert_eq!(
        one(&heard, "token"),
        "tok-1",
        "the new token, not the stale one"
    );
    assert_eq!(one(&heard, "startup"), "tok-1");
    assert_eq!(
        one(&heard, "cwd"),
        rite.dir.path().display().to_string(),
        "the home dir"
    );
    assert_eq!(one(&heard, "sid"), pid.to_string(), "a session of its own");
    rite.release(pid);
}

#[test]
fn without_a_token_no_stale_one_is_passed_on() {
    let rite = Rite::new();
    let session = Session {
        bus: None,
        env: rite.env("caller"),
    };
    let plan = Plan {
        cwd: Some(bin()),
        ..anoint(&[])
    };
    let pid = launch(&plan, None, &session).unwrap().pid.unwrap();
    let heard = rite.heard();
    assert_eq!(one(&heard, "token"), "");
    assert_eq!(
        one(&heard, "cwd"),
        bin().display().to_string(),
        "the entry's Path"
    );
    rite.release(pid);
}

/// A user manager that knows `env` and accepts transient units.
fn systemd(env: Vec<(OsString, OsString)>) -> FakeBus {
    let env: Vec<Value> = env
        .into_iter()
        .map(|(k, v)| Value::Str(format!("{}={}", k.to_string_lossy(), v.to_string_lossy())))
        .collect();
    FakeBus::start(move |call| match call.member.as_deref() {
        Some("Get") => Answer::Return(vec![Value::Variant(Box::new(Value::Array(
            "s".into(),
            env.clone(),
        )))]),
        Some("StartTransientUnit") => {
            Answer::Return(vec![Value::Path("/org/freedesktop/systemd1/job/7".into())])
        }
        _ => Answer::Error("org.freedesktop.DBus.Error.UnknownMethod", "no".into()),
    })
}

fn property<'a>(properties: &'a [Value], name: &str) -> &'a Value {
    properties
        .iter()
        .find_map(|p| match p {
            Value::Struct(f) if f[0] == Value::Str(name.into()) => match &f[1] {
                Value::Variant(v) => Some(&**v),
                _ => None,
            },
            _ => None,
        })
        .unwrap_or_else(|| panic!("no property {name}"))
}

#[test]
fn with_a_user_manager_the_app_gets_the_session_environment_and_a_scope() {
    let rite = Rite::new();
    let bus = systemd(rite.env("session"));
    let session = Session {
        bus: Some(bus.address.clone()),
        env: rite.env("caller"),
    };
    let launched = launch(&anoint(&[]), Some("tok-2"), &session).unwrap();
    let pid = launched.pid.unwrap();
    let heard = rite.heard();
    assert_eq!(
        one(&heard, "marker"),
        "session",
        "systemd's environment wins"
    );
    assert_eq!(one(&heard, "token"), "tok-2");

    let get = bus.wait_for("Get");
    assert_eq!(
        get.body,
        [
            Value::Str("org.freedesktop.systemd1.Manager".into()),
            Value::Str("Environment".into())
        ]
    );
    let unit = bus.wait_for("StartTransientUnit");
    assert_eq!(
        unit.destination.as_deref(),
        Some("org.freedesktop.systemd1")
    );
    let [
        Value::Str(name),
        Value::Str(mode),
        Value::Array(_, properties),
        _,
    ] = &unit.body[..]
    else {
        panic!("{:?}", unit.body);
    };
    assert!(
        name.starts_with("app-logis-forge\\x2danoint-") && name.ends_with(".scope"),
        "{name}"
    );
    assert_eq!(launched.via, Via::Scope(name.clone()));
    assert_eq!(mode, "fail");
    assert_eq!(
        property(properties, "PIDs"),
        &Value::Array("u".into(), vec![Value::U32(pid)]),
        "the process itself goes into the scope"
    );
    assert_eq!(
        property(properties, "Slice"),
        &Value::Str("app.slice".into())
    );
    assert_eq!(
        property(properties, "Description"),
        &Value::Str("Anoint".into())
    );
    rite.release(pid);
}

#[test]
fn a_program_not_on_the_path_is_not_found_and_nothing_starts() {
    let rite = Rite::new();
    let session = Session {
        bus: None,
        env: rite.env("caller"),
    };
    let plan = Plan {
        argv: vec!["forge-no-such-program".into()],
        ..anoint(&[])
    };
    assert!(matches!(
        launch(&plan, None, &session),
        Err(LaunchError::NotFound(p)) if p == "forge-no-such-program"
    ));
}

/// A bus on which `activatable` names can be activated.
fn activation(activatable: &'static [&'static str]) -> FakeBus {
    FakeBus::start(move |call| match call.member.as_deref() {
        Some("ListActivatableNames") => Answer::Return(vec![Value::Array(
            "s".into(),
            activatable
                .iter()
                .map(|n| Value::Str((*n).into()))
                .collect(),
        )]),
        Some("NameHasOwner") => Answer::Return(vec![Value::Bool(false)]),
        Some("Activate" | "ActivateAction") => Answer::Return(vec![]),
        _ => Answer::Error("org.freedesktop.DBus.Error.UnknownMethod", "no".into()),
    })
}

fn token_data(token: &str) -> Value {
    let entry = |k: &str| {
        Value::Entry(
            Box::new(Value::Str(k.into())),
            Box::new(Value::Variant(Box::new(Value::Str(token.into())))),
        )
    };
    Value::Array(
        "{sv}".into(),
        vec![entry("activation-token"), entry("desktop-startup-id")],
    )
}

#[test]
fn a_dbus_activatable_app_is_activated_with_the_token() {
    let bus = activation(&["org.forge.Bus"]);
    let session = Session {
        bus: Some(bus.address.clone()),
        env: Vec::new(),
    };
    let plan = Plan {
        app_id: "org.forge.Bus".into(),
        name: "Bus Spirit".into(),
        dbus: true,
        ..Plan::default()
    };
    let launched = launch(&plan, Some("tok-3"), &session).unwrap();
    assert_eq!((launched.via, launched.pid), (Via::Bus, None));
    let call = bus.wait_for("Activate");
    assert_eq!(call.destination.as_deref(), Some("org.forge.Bus"));
    assert_eq!(call.path.as_deref(), Some("/org/forge/Bus"));
    assert_eq!(
        call.interface.as_deref(),
        Some("org.freedesktop.Application")
    );
    assert_eq!(call.body, [token_data("tok-3")]);

    let wake = Plan {
        action: Some("wake".into()),
        ..plan
    };
    launch(&wake, Some("tok-4"), &session).unwrap();
    let call = bus.wait_for("ActivateAction");
    assert_eq!(
        call.body,
        [
            Value::Str("wake".into()),
            Value::Array("v".into(), vec![]),
            token_data("tok-4")
        ]
    );
}

#[test]
fn a_dbus_app_the_bus_cannot_start_runs_its_exec_or_fails() {
    let rite = Rite::new();
    let bus = activation(&[]);
    let session = Session {
        bus: Some(bus.address.clone()),
        env: rite.env("caller"),
    };
    let plan = Plan {
        dbus: true,
        ..anoint(&["--fallback"])
    };
    let launched = launch(&plan, None, &session).unwrap();
    assert_eq!(
        launched.via,
        Via::Plain,
        "this bus has no user manager either"
    );
    assert!(!launched.warnings.is_empty(), "the refused scope is noted");
    let heard = rite.heard();
    assert_eq!(heard["arg"], ["--fallback"]);
    rite.release(launched.pid.unwrap());

    let only_dbus = Plan {
        argv: Vec::new(),
        ..plan
    };
    assert!(matches!(
        launch(&only_dbus, None, &session),
        Err(LaunchError::NotActivatable(_))
    ));
}
