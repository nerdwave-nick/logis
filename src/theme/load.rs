//! Finding a theme by name and reading it with what it extends: a user
//! file in the themes directory (`themes/` beside the config) wins over a
//! bundled theme of the same name; a theme may extend one other theme, and
//! every theme lies over the bundled default, which gives every key.

use std::fmt;
use std::io;
use std::path::{Path, PathBuf};

use super::Theme;
use super::doc::{Doc, Merge};
use super::resolve::resolve;
use crate::config::{Invalid, parse_jsonc};

/// The themes compiled into logis: name and text.
pub const BUNDLED: [(&str, &str); 2] = [
    (
        "forge-crimson-brass",
        include_str!("../../themes/forge-crimson-brass.jsonc"),
    ),
    (
        "cogitator-phosphor",
        include_str!("../../themes/cogitator-phosphor.jsonc"),
    ),
];

/// The bundled theme every other theme falls back to.
pub const DEFAULT: &str = "forge-crimson-brass";

/// Why a theme could not be worn.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ThemeError {
    /// The file (or bundled theme) the problem is in.
    pub source: String,
    pub problem: String,
}

impl fmt::Display for ThemeError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}: {}", self.source, self.problem)
    }
}

impl std::error::Error for ThemeError {}

/// Where themes are looked for.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Themes {
    /// The user's themes directory, if there is one.
    dir: Option<PathBuf>,
}

/// One theme's text and where it came from.
struct Found {
    name: String,
    /// The user file, or `None` for a bundled theme.
    path: Option<PathBuf>,
    text: String,
}

impl Found {
    fn source(&self) -> String {
        match &self.path {
            Some(path) => path.display().to_string(),
            None => format!("the bundled theme '{}'", self.name),
        }
    }

    fn error(&self, problem: impl Into<String>) -> ThemeError {
        ThemeError {
            source: self.source(),
            problem: problem.into(),
        }
    }

    fn parse(&self) -> Result<Doc, ThemeError> {
        parse_jsonc::<Doc>(&self.text, "theme")
            .map(Option::unwrap_or_default)
            .map_err(
                |Invalid {
                     line,
                     column,
                     message,
                 }| ThemeError {
                    source: format!("{}:{line}:{column}", self.source()),
                    problem: message,
                },
            )
    }
}

impl Themes {
    /// User themes in `dir` (`themes/` beside the config file), if any.
    pub fn new(dir: Option<PathBuf>) -> Self {
        Self { dir }
    }

    /// The themes directory beside the config file `config`.
    pub fn beside(config: Option<&Path>) -> Self {
        Self::new(config.and_then(Path::parent).map(|d| d.join("themes")))
    }

    /// The user's themes directory.
    pub fn dir(&self) -> Option<&Path> {
        self.dir.as_deref()
    }

    /// The theme `name`, resolved over what it extends and the default.
    pub fn load(&self, name: &str) -> Result<Theme, ThemeError> {
        let found = self.find(name)?;
        let doc = found.parse()?;
        let mut chain = vec![doc.clone()];
        if let Some(parent) = &doc.extends {
            chain.push(self.parent(&found, parent)?);
        }
        let default = bundled(DEFAULT).expect("the default theme is bundled");
        chain.push(default.parse()?);
        let merged = chain
            .into_iter()
            .reduce(Merge::over)
            .expect("the chain holds the theme");
        let dir = found
            .path
            .as_deref()
            .and_then(Path::parent)
            .map(Path::to_owned);
        resolve(name, dir, merged).map_err(|e| found.error(e))
    }

    /// The theme `child` extends: by name, except that a theme extending
    /// its own name extends the bundled theme of that name (a user file
    /// amending a bundled theme). One level only.
    fn parent(&self, child: &Found, name: &str) -> Result<Doc, ThemeError> {
        let found = if name == child.name {
            bundled(name).ok_or_else(|| {
                child.error(format!(
                    "\"extends\": \"{name}\" names this theme itself, and no bundled theme \
                     bears that name"
                ))
            })?
        } else {
            self.find(name)
                .map_err(|e| child.error(format!("\"extends\": {}", e.problem)))?
        };
        let doc = found.parse()?;
        match &doc.extends {
            None => Ok(doc),
            Some(next) if *next == child.name => Err(child.error(format!(
                "\"extends\" goes round in a circle: '{}' extends '{name}', which extends '{next}'",
                child.name
            ))),
            Some(next) => Err(child.error(format!(
                "'{name}' extends '{next}' in turn; a theme extends at most one level"
            ))),
        }
    }

    /// The user file `name` if it exists, else the bundled theme.
    fn find(&self, name: &str) -> Result<Found, ThemeError> {
        if let Some(dir) = &self.dir {
            let path = dir.join(format!("{name}.jsonc"));
            match std::fs::read_to_string(&path) {
                Ok(text) => {
                    return Ok(Found {
                        name: name.to_owned(),
                        path: Some(path),
                        text,
                    });
                }
                Err(e) if e.kind() == io::ErrorKind::NotFound => {}
                Err(e) => {
                    return Err(ThemeError {
                        source: path.display().to_string(),
                        problem: e.to_string(),
                    });
                }
            }
        }
        bundled(name).ok_or_else(|| ThemeError {
            source: format!("theme '{name}'"),
            problem: self.unknown(),
        })
    }

    fn unknown(&self) -> String {
        let bundled: Vec<_> = BUNDLED.iter().map(|(n, _)| *n).collect();
        let place = match &self.dir {
            Some(dir) => format!(" nor in {}", dir.display()),
            None => String::new(),
        };
        format!("no such theme is bundled ({}){place}", bundled.join(", "))
    }
}

fn bundled(name: &str) -> Option<Found> {
    BUNDLED
        .iter()
        .find(|(n, _)| *n == name)
        .map(|(n, text)| Found {
            name: (*n).to_owned(),
            path: None,
            text: (*text).to_owned(),
        })
}
