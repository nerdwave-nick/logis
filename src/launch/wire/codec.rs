//! The byte-level coding of D-Bus values: alignment, both byte orders when
//! reading, signatures split into complete types.

use super::{Value, WireError};

pub(super) fn big_endian(mark: u8) -> Result<bool, WireError> {
    match mark {
        b'l' => Ok(false),
        b'B' => Ok(true),
        other => Err(WireError(format!("no byte order ({other:#x})"))),
    }
}

/// The alignment of a type by its signature's first character.
fn alignment(sig: &str) -> usize {
    match sig.as_bytes().first() {
        Some(b'n' | b'q') => 2,
        Some(b'b' | b'u' | b'i' | b's' | b'o' | b'a' | b'h') => 4,
        Some(b'x' | b't' | b'd' | b'(' | b'{') => 8,
        _ => 1,
    }
}

/// Splits the first complete type off a signature.
pub(super) fn first_type(sig: &str) -> Result<(&str, &str), WireError> {
    let bytes = sig.as_bytes();
    let bad = || WireError(format!("the signature '{sig}' is malformed"));
    let end = match bytes.first().ok_or_else(bad)? {
        b'a' => 1 + first_type(&sig[1..])?.0.len(),
        open @ (b'(' | b'{') => {
            let close = if *open == b'(' { b')' } else { b'}' };
            let mut depth = 0;
            let mut end = None;
            for (i, &b) in bytes.iter().enumerate() {
                if b == *open {
                    depth += 1;
                } else if b == close {
                    depth -= 1;
                    if depth == 0 {
                        end = Some(i + 1);
                        break;
                    }
                }
            }
            end.ok_or_else(bad)?
        }
        _ => 1,
    };
    Ok(sig.split_at(end))
}

#[derive(Default)]
pub(super) struct Enc {
    pub(super) buf: Vec<u8>,
}

impl Enc {
    pub(super) fn pad(&mut self, align: usize) {
        let len = self.buf.len().next_multiple_of(align);
        self.buf.resize(len, 0);
    }

    pub(super) fn u32(&mut self, v: u32) {
        self.pad(4);
        self.buf.extend(v.to_le_bytes());
    }

    fn string(&mut self, s: &str) {
        self.u32(s.len() as u32);
        self.buf.extend(s.as_bytes());
        self.buf.push(0);
    }

    pub(super) fn value(&mut self, value: &Value) {
        match value {
            Value::Byte(b) => self.buf.push(*b),
            Value::Bool(b) => self.u32(u32::from(*b)),
            Value::U32(v) => self.u32(*v),
            Value::Int(v) => {
                self.pad(8);
                self.buf.extend(v.to_le_bytes());
            }
            Value::Double(v) => {
                self.pad(8);
                self.buf.extend(v.to_le_bytes());
            }
            Value::Str(s) | Value::Path(s) => self.string(s),
            Value::Sig(s) => {
                self.buf.push(s.len() as u8);
                self.buf.extend(s.as_bytes());
                self.buf.push(0);
            }
            Value::Array(element, items) => {
                self.u32(0);
                let at = self.buf.len() - 4;
                self.pad(alignment(element));
                let start = self.buf.len();
                for item in items {
                    self.value(item);
                }
                let len = (self.buf.len() - start) as u32;
                self.buf[at..at + 4].copy_from_slice(&len.to_le_bytes());
            }
            Value::Struct(fields) => {
                self.pad(8);
                for field in fields {
                    self.value(field);
                }
            }
            Value::Entry(key, value) => {
                self.pad(8);
                self.value(key);
                self.value(value);
            }
            Value::Variant(inner) => {
                self.value(&Value::Sig(inner.signature()));
                self.value(inner);
            }
        }
    }
}

pub(super) struct Dec<'a> {
    pub(super) data: &'a [u8],
    pub(super) pos: usize,
    pub(super) big: bool,
    pub(super) depth: usize,
}

/// How deeply containers may nest (the spec allows 64 in all).
const MAX_DEPTH: usize = 64;

impl Dec<'_> {
    fn short() -> WireError {
        WireError("it ends too early".into())
    }

    pub(super) fn align(&mut self, align: usize) -> Result<(), WireError> {
        let pos = self.pos.next_multiple_of(align);
        if pos > self.data.len() {
            return Err(Self::short());
        }
        self.pos = pos;
        Ok(())
    }

    fn take(&mut self, n: usize) -> Result<&[u8], WireError> {
        let end = self.pos.checked_add(n).filter(|&e| e <= self.data.len());
        let end = end.ok_or_else(Self::short)?;
        let bytes = &self.data[self.pos..end];
        self.pos = end;
        Ok(bytes)
    }

    pub(super) fn word_at(&self, at: usize) -> u32 {
        let b: [u8; 4] = self.data[at..at + 4].try_into().expect("four bytes");
        if self.big {
            u32::from_be_bytes(b)
        } else {
            u32::from_le_bytes(b)
        }
    }

    fn fixed<const N: usize>(&mut self) -> Result<[u8; N], WireError> {
        self.align(N)?;
        let mut b: [u8; N] = self.take(N)?.try_into().expect("N bytes");
        if self.big {
            b.reverse();
        }
        Ok(b)
    }

    fn u32(&mut self) -> Result<u32, WireError> {
        self.fixed::<4>().map(u32::from_le_bytes)
    }

    fn text(&mut self, len: usize) -> Result<String, WireError> {
        let bytes = self.take(len + 1)?;
        if bytes[len] != 0 {
            return Err(WireError("a string without its NUL".into()));
        }
        String::from_utf8(bytes[..len].to_vec())
            .map_err(|_| WireError("a string not in UTF-8".into()))
    }

    /// One value of the single complete type `sig`.
    pub(super) fn value(&mut self, sig: &str) -> Result<Value, WireError> {
        self.depth += 1;
        if self.depth > MAX_DEPTH {
            return Err(WireError("nested too deeply".into()));
        }
        let value = self.inner(sig);
        self.depth -= 1;
        value
    }

    fn inner(&mut self, sig: &str) -> Result<Value, WireError> {
        Ok(match sig.as_bytes()[0] {
            b'y' => Value::Byte(self.take(1)?[0]),
            b'b' => Value::Bool(self.u32()? != 0),
            b'u' => Value::U32(self.u32()?),
            b'i' => Value::Int(i64::from(self.u32()? as i32)),
            b'h' => Value::Int(i64::from(self.u32()?)),
            b'n' => Value::Int(i64::from(i16::from_le_bytes(self.fixed::<2>()?))),
            b'q' => Value::Int(i64::from(u16::from_le_bytes(self.fixed::<2>()?))),
            b'x' | b't' => Value::Int(i64::from_le_bytes(self.fixed::<8>()?)),
            b'd' => Value::Double(f64::from_le_bytes(self.fixed::<8>()?)),
            b's' => {
                let len = self.u32()? as usize;
                Value::Str(self.text(len)?)
            }
            b'o' => {
                let len = self.u32()? as usize;
                Value::Path(self.text(len)?)
            }
            b'g' => {
                let len = usize::from(self.take(1)?[0]);
                Value::Sig(self.text(len)?)
            }
            b'v' => {
                let len = usize::from(self.take(1)?[0]);
                let inner = self.text(len)?;
                let (one, rest) = first_type(&inner)?;
                if !rest.is_empty() {
                    return Err(WireError("a variant of more than one type".into()));
                }
                Value::Variant(Box::new(self.value(one)?))
            }
            b'a' => {
                let element = &sig[1..];
                let len = self.u32()? as usize;
                self.align(alignment(element))?;
                let end = self.pos.checked_add(len).filter(|&e| e <= self.data.len());
                let end = end.ok_or_else(Self::short)?;
                let mut items = Vec::new();
                while self.pos < end {
                    items.push(self.value(element)?);
                }
                if self.pos != end {
                    return Err(WireError("an array overruns its length".into()));
                }
                Value::Array(element.to_owned(), items)
            }
            open @ (b'(' | b'{') => {
                self.align(8)?;
                let mut rest = &sig[1..sig.len() - 1];
                let mut fields = Vec::new();
                while !rest.is_empty() {
                    let (one, more) = first_type(rest)?;
                    fields.push(self.value(one)?);
                    rest = more;
                }
                match (open, <[Value; 2]>::try_from(fields)) {
                    (b'{', Ok([k, v])) => Value::Entry(Box::new(k), Box::new(v)),
                    (b'{', Err(_)) => return Err(WireError("a dict entry not of two".into())),
                    (_, Ok(two)) => Value::Struct(two.into()),
                    (_, Err(fields)) => Value::Struct(fields),
                }
            }
            other => {
                return Err(WireError(format!(
                    "the type '{}' is unknown",
                    other as char
                )));
            }
        })
    }
}
