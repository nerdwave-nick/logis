//! Command-line parsing. Flag names stay conventional (spec §4.6).

use std::ffi::OsString;

/// What one invocation of `logis` was asked to do.
#[derive(Debug, PartialEq, Eq)]
pub enum Command {
    /// Print the name and version.
    Version,
    /// Verify the Wayland session can host the cogitator.
    Run,
    /// `daemon --debug-panel`: show the renderer's static debug panel.
    DebugPanel,
}

/// Parses the arguments after the program name.
/// An unknown argument is returned, lossily decoded, as the error.
pub fn parse(args: impl IntoIterator<Item = OsString>) -> Result<Command, String> {
    let args: Vec<OsString> = args.into_iter().collect();
    if args == ["daemon", "--debug-panel"] {
        return Ok(Command::DebugPanel);
    }
    let mut command = Command::Run;
    for (i, arg) in args.into_iter().enumerate() {
        match arg.to_str() {
            Some("--version" | "-V") if i == 0 => command = Command::Version,
            _ => return Err(arg.to_string_lossy().into_owned()),
        }
    }
    Ok(command)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse_strs(args: &[&str]) -> Result<Command, String> {
        parse(args.iter().map(OsString::from))
    }

    #[test]
    fn no_arguments_runs() {
        assert_eq!(parse_strs(&[]), Ok(Command::Run));
    }

    #[test]
    fn version_flags_ask_for_the_version() {
        assert_eq!(parse_strs(&["--version"]), Ok(Command::Version));
        assert_eq!(parse_strs(&["-V"]), Ok(Command::Version));
    }

    #[test]
    fn daemon_debug_panel_shows_the_debug_panel() {
        assert_eq!(
            parse_strs(&["daemon", "--debug-panel"]),
            Ok(Command::DebugPanel)
        );
    }

    #[test]
    fn daemon_without_debug_panel_is_not_yet_known() {
        assert_eq!(parse_strs(&["daemon"]), Err("daemon".to_owned()));
        assert_eq!(
            parse_strs(&["--debug-panel"]),
            Err("--debug-panel".to_owned())
        );
    }

    #[test]
    fn an_unknown_argument_is_returned_as_the_error() {
        assert_eq!(parse_strs(&["--dmenu"]), Err("--dmenu".to_owned()));
        assert_eq!(parse_strs(&["--version", "extra"]), Err("extra".to_owned()));
    }
}
