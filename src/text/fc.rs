//! A thin, safe binding to the few libfontconfig calls logis needs: load a
//! configuration, substitute and match a family, sort its fallbacks, and read
//! file, index and coverage from the resulting patterns. Every pointer is
//! owned by exactly one wrapper and freed in its `Drop`.

use std::ffi::{CStr, CString, c_char, c_int, c_uint};
use std::os::unix::ffi::{OsStrExt, OsStringExt};
use std::path::{Path, PathBuf};
use std::ptr::{self, NonNull};

#[repr(C)]
struct FcConfig {
    _opaque: [u8; 0],
}

#[repr(C)]
struct FcPattern {
    _opaque: [u8; 0],
}

#[repr(C)]
struct FcCharSet {
    _opaque: [u8; 0],
}

/// `FcFontSet` is public in fontconfig.h and stable since 2.0.
#[repr(C)]
struct FcFontSet {
    nfont: c_int,
    sfont: c_int,
    fonts: *mut *mut FcPattern,
}

type FcBool = c_int;
const FC_TRUE: FcBool = 1;
const FC_RESULT_MATCH: c_int = 0;
const FC_MATCH_PATTERN: c_int = 0;

const FC_FAMILY: &CStr = c"family";
const FC_FILE: &CStr = c"file";
const FC_INDEX: &CStr = c"index";
const FC_CHARSET: &CStr = c"charset";
const FC_WEIGHT: &CStr = c"weight";

#[link(name = "fontconfig")]
unsafe extern "C" {
    fn FcInitLoadConfigAndFonts() -> *mut FcConfig;
    fn FcConfigCreate() -> *mut FcConfig;
    fn FcConfigDestroy(config: *mut FcConfig);
    fn FcConfigParseAndLoadFromMemory(
        config: *mut FcConfig,
        buffer: *const c_char,
        complain: FcBool,
    ) -> FcBool;
    fn FcConfigAppFontAddFile(config: *mut FcConfig, file: *const c_char) -> FcBool;
    fn FcConfigSubstitute(config: *mut FcConfig, p: *mut FcPattern, kind: c_int) -> FcBool;
    fn FcDefaultSubstitute(p: *mut FcPattern);
    fn FcPatternCreate() -> *mut FcPattern;
    fn FcPatternDestroy(p: *mut FcPattern);
    fn FcPatternAddString(p: *mut FcPattern, object: *const c_char, s: *const c_char) -> FcBool;
    fn FcPatternAddInteger(p: *mut FcPattern, object: *const c_char, i: c_int) -> FcBool;
    fn FcWeightFromOpenType(weight: c_int) -> c_int;
    fn FcPatternGetString(
        p: *const FcPattern,
        object: *const c_char,
        n: c_int,
        s: *mut *const c_char,
    ) -> c_int;
    fn FcPatternGetInteger(
        p: *const FcPattern,
        object: *const c_char,
        n: c_int,
        i: *mut c_int,
    ) -> c_int;
    fn FcPatternGetCharSet(
        p: *const FcPattern,
        object: *const c_char,
        n: c_int,
        c: *mut *mut FcCharSet,
    ) -> c_int;
    fn FcCharSetHasChar(c: *const FcCharSet, ucs4: c_uint) -> FcBool;
    fn FcFontMatch(config: *mut FcConfig, p: *mut FcPattern, result: *mut c_int) -> *mut FcPattern;
    fn FcFontSort(
        config: *mut FcConfig,
        p: *mut FcPattern,
        trim: FcBool,
        csp: *mut *mut FcCharSet,
        result: *mut c_int,
    ) -> *mut FcFontSet;
    fn FcFontSetDestroy(s: *mut FcFontSet);
}

/// One face fontconfig resolved: a file and the face index inside it.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct FontFile {
    /// The font file.
    pub path: PathBuf,
    /// The face within it (0 unless the file is a collection).
    pub index: u32,
}

/// A loaded fontconfig configuration: its rules (aliases included) and the
/// fonts it knows. Only metadata; no font file is opened for drawing here.
pub struct Config(NonNull<FcConfig>);

impl Config {
    /// The user's configuration, exactly as other fontconfig clients see it:
    /// system and user `fonts.conf`, aliases and the font cache.
    pub fn system() -> Option<Self> {
        // SAFETY: no preconditions; a null return means failure.
        NonNull::new(unsafe { FcInitLoadConfigAndFonts() }).map(Self)
    }

    /// A configuration with only the rules in `xml` (a `<fontconfig>`
    /// document) and only the font `files`, nothing from the system.
    pub fn isolated(xml: &str, files: &[&Path]) -> Result<Self, String> {
        // SAFETY: no preconditions; a null return means failure.
        let config = NonNull::new(unsafe { FcConfigCreate() })
            .map(Self)
            .ok_or("fontconfig could not create a configuration")?;
        let xml = CString::new(xml).map_err(|_| "the rules contain a NUL byte")?;
        // SAFETY: the config is live and the buffer is a NUL-terminated string.
        if unsafe { FcConfigParseAndLoadFromMemory(config.0.as_ptr(), xml.as_ptr(), FC_TRUE) }
            != FC_TRUE
        {
            return Err("fontconfig rejected the rules".into());
        }
        for file in files {
            let name = CString::new(file.as_os_str().as_bytes())
                .map_err(|_| format!("{} contains a NUL byte", file.display()))?;
            // SAFETY: as above; fontconfig copies the name.
            if unsafe { FcConfigAppFontAddFile(config.0.as_ptr(), name.as_ptr()) } != FC_TRUE {
                return Err(format!("fontconfig could not read {}", file.display()));
            }
        }
        Ok(config)
    }

    /// The best face for `family` after substitution (aliases, defaults),
    /// with its family name as fontconfig knows it.
    pub fn best(&self, family: &str) -> Option<(FontFile, String)> {
        self.best_of(&[family], None)
    }

    /// The best face for the first of `families` that is installed (they
    /// are matched as one list, in order, as fontconfig clients do), at
    /// the OpenType `weight` if given.
    pub fn best_of(&self, families: &[&str], weight: Option<u16>) -> Option<(FontFile, String)> {
        let pattern = self.query(families, weight)?;
        let mut result = 0;
        // SAFETY: config and pattern are live; the match is a new pattern we own.
        let found =
            NonNull::new(unsafe { FcFontMatch(self.0.as_ptr(), pattern.0.as_ptr(), &mut result) })
                .map(Pattern)?;
        Some((found.file()?, found.string(FC_FAMILY)?))
    }

    /// Every face that adds coverage for `family`, best first: fontconfig's
    /// own fallback order, honouring the configured aliases.
    pub fn fallbacks(&self, family: &str) -> Option<FontSet> {
        let pattern = self.query(&[family], None)?;
        let mut result = 0;
        // SAFETY: config and pattern are live; null `csp` asks for no charset.
        let set = unsafe {
            FcFontSort(
                self.0.as_ptr(),
                pattern.0.as_ptr(),
                FC_TRUE,
                ptr::null_mut(),
                &mut result,
            )
        };
        NonNull::new(set).map(FontSet)
    }

    /// A pattern for `families` (in order) at `weight`, with the
    /// configuration's substitutions applied.
    fn query(&self, families: &[&str], weight: Option<u16>) -> Option<Pattern> {
        // SAFETY: no preconditions; a null return means failure.
        let pattern = NonNull::new(unsafe { FcPatternCreate() }).map(Pattern)?;
        for family in families {
            let family = CString::new(*family).ok()?;
            // SAFETY: the pattern is live; fontconfig copies the string and
            // appends it to the family list.
            let added = unsafe {
                FcPatternAddString(pattern.0.as_ptr(), FC_FAMILY.as_ptr(), family.as_ptr())
            };
            if added != FC_TRUE {
                return None;
            }
        }
        // SAFETY: the pattern and config are live.
        unsafe {
            if let Some(weight) = weight {
                let fc = FcWeightFromOpenType(c_int::from(weight));
                if FcPatternAddInteger(pattern.0.as_ptr(), FC_WEIGHT.as_ptr(), fc) != FC_TRUE {
                    return None;
                }
            }
            FcConfigSubstitute(self.0.as_ptr(), pattern.0.as_ptr(), FC_MATCH_PATTERN);
            FcDefaultSubstitute(pattern.0.as_ptr());
        }
        Some(pattern)
    }
}

// SAFETY: fontconfig objects have no thread affinity, and fontconfig has
// been thread-safe since 2.10.91 (reference counts and its cache use atomics
// and locks). A `Config` is moved, never shared: it is not `Sync`, so one
// thread at a time calls into it, and it is freed once, by its owner.
unsafe impl Send for Config {}

impl Drop for Config {
    fn drop(&mut self) {
        // SAFETY: we own the only reference; patterns and sets we hand out
        // are independent copies.
        unsafe { FcConfigDestroy(self.0.as_ptr()) }
    }
}

/// An owned pattern.
struct Pattern(NonNull<FcPattern>);

impl Pattern {
    fn file(&self) -> Option<FontFile> {
        file_of(self.0.as_ptr())
    }

    fn string(&self, object: &CStr) -> Option<String> {
        string_of(self.0.as_ptr(), object).map(|b| String::from_utf8_lossy(&b).into_owned())
    }
}

impl Drop for Pattern {
    fn drop(&mut self) {
        // SAFETY: we own this reference.
        unsafe { FcPatternDestroy(self.0.as_ptr()) }
    }
}

/// An owned, sorted list of faces (the result of a fallback sort).
pub struct FontSet(NonNull<FcFontSet>);

impl FontSet {
    /// The first face in the list whose coverage includes `c`.
    pub fn first_covering(&self, c: char) -> Option<FontFile> {
        self.patterns().find(|&p| covers(p, c)).and_then(file_of)
    }

    fn patterns(&self) -> impl Iterator<Item = *const FcPattern> + '_ {
        // SAFETY: the set is live; `fonts` holds `nfont` valid patterns.
        let set = unsafe { self.0.as_ref() };
        let count = usize::try_from(set.nfont).unwrap_or(0);
        (0..count).map(move |i| unsafe { *set.fonts.add(i) } as *const FcPattern)
    }
}

// SAFETY: as for `Config`: the set and its patterns are owned here alone,
// moved between threads but never shared, and freed once.
unsafe impl Send for FontSet {}

impl Drop for FontSet {
    fn drop(&mut self) {
        // SAFETY: we own the set and every pattern in it.
        unsafe { FcFontSetDestroy(self.0.as_ptr()) }
    }
}

fn covers(pattern: *const FcPattern, c: char) -> bool {
    let mut charset = ptr::null_mut();
    // SAFETY: the pattern is live; the charset stays owned by the pattern.
    unsafe {
        FcPatternGetCharSet(pattern, FC_CHARSET.as_ptr(), 0, &mut charset) == FC_RESULT_MATCH
            && FcCharSetHasChar(charset, c as c_uint) == FC_TRUE
    }
}

fn file_of(pattern: *const FcPattern) -> Option<FontFile> {
    let path = PathBuf::from(std::ffi::OsString::from_vec(string_of(pattern, FC_FILE)?));
    let mut index: c_int = 0;
    // SAFETY: the pattern is live. A missing index means face 0.
    unsafe { FcPatternGetInteger(pattern, FC_INDEX.as_ptr(), 0, &mut index) };
    // The upper 16 bits name a variable font's instance; the face is below.
    let index = u32::try_from(index).unwrap_or(0) & 0xffff;
    Some(FontFile { path, index })
}

fn string_of(pattern: *const FcPattern, object: &CStr) -> Option<Vec<u8>> {
    let mut value: *const c_char = ptr::null();
    // SAFETY: the pattern is live; the string stays owned by the pattern and
    // is copied before returning.
    unsafe {
        if FcPatternGetString(pattern, object.as_ptr(), 0, &mut value) != FC_RESULT_MATCH
            || value.is_null()
        {
            return None;
        }
        Some(CStr::from_ptr(value).to_bytes().to_vec())
    }
}
