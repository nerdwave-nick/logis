//! Standard base64 (RFC 4648, with padding) for carrying an item's original
//! bytes inside a JSON frame. Small enough that a crate would not earn its
//! place (spec §4.11).

const ALPHABET: &[u8; 64] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

/// Encodes bytes as padded standard base64.
pub fn encode(bytes: &[u8]) -> String {
    let mut out = String::with_capacity(bytes.len().div_ceil(3) * 4);
    for chunk in bytes.chunks(3) {
        let b = [
            chunk[0],
            *chunk.get(1).unwrap_or(&0),
            *chunk.get(2).unwrap_or(&0),
        ];
        let n = u32::from(b[0]) << 16 | u32::from(b[1]) << 8 | u32::from(b[2]);
        for i in 0..4 {
            if i <= chunk.len() {
                out.push(char::from(ALPHABET[(n >> (18 - 6 * i) & 63) as usize]));
            } else {
                out.push('=');
            }
        }
    }
    out
}

/// Decodes padded standard base64; anything else is refused.
pub fn decode(text: &str) -> Result<Vec<u8>, String> {
    let bytes = text.as_bytes();
    if !bytes.len().is_multiple_of(4) {
        return Err(format!(
            "base64 length {} is not a multiple of 4",
            bytes.len()
        ));
    }
    let mut out = Vec::with_capacity(bytes.len() / 4 * 3);
    let quads = bytes.len() / 4;
    for (q, quad) in bytes.chunks(4).enumerate() {
        let padding = quad.iter().rev().take_while(|&&c| c == b'=').count();
        if padding > 2 || (padding > 0 && q + 1 != quads) {
            return Err("misplaced base64 padding".to_owned());
        }
        let mut n = 0u32;
        for &c in &quad[..4 - padding] {
            n = n << 6
                | u32::from(sextet(c).ok_or_else(|| format!("invalid base64 byte {c:#04x}"))?);
        }
        n <<= 6 * padding;
        let decoded = [(n >> 16) as u8, (n >> 8) as u8, n as u8];
        let keep = 3 - padding;
        if decoded[keep..].iter().any(|&b| b != 0) {
            return Err("non-canonical base64 padding bits".to_owned());
        }
        out.extend_from_slice(&decoded[..keep]);
    }
    Ok(out)
}

fn sextet(c: u8) -> Option<u8> {
    match c {
        b'A'..=b'Z' => Some(c - b'A'),
        b'a'..=b'z' => Some(c - b'a' + 26),
        b'0'..=b'9' => Some(c - b'0' + 52),
        b'+' => Some(62),
        b'/' => Some(63),
        _ => None,
    }
}

/// `#[serde(with = "base64::serde_bytes")]` for byte vectors.
pub mod serde_bytes {
    use serde::{Deserialize, Deserializer, Serializer, de::Error};

    pub fn serialize<S: Serializer>(bytes: &[u8], serializer: S) -> Result<S::Ok, S::Error> {
        serializer.serialize_str(&super::encode(bytes))
    }

    pub fn deserialize<'de, D: Deserializer<'de>>(deserializer: D) -> Result<Vec<u8>, D::Error> {
        let text = <std::borrow::Cow<'de, str>>::deserialize(deserializer)?;
        super::decode(&text).map_err(D::Error::custom)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn encodes_the_rfc_4648_test_vectors() {
        let vectors = [
            ("", ""),
            ("f", "Zg=="),
            ("fo", "Zm8="),
            ("foo", "Zm9v"),
            ("foob", "Zm9vYg=="),
            ("fooba", "Zm9vYmE="),
            ("foobar", "Zm9vYmFy"),
        ];
        for (plain, encoded) in vectors {
            assert_eq!(encode(plain.as_bytes()), encoded, "{plain:?}");
            assert_eq!(
                decode(encoded).as_deref(),
                Ok(plain.as_bytes()),
                "{encoded:?}"
            );
        }
    }

    #[test]
    fn every_byte_value_round_trips_at_every_length() {
        let all: Vec<u8> = (0..=255).collect();
        for len in 0..all.len() {
            let bytes = &all[all.len() - len..];
            assert_eq!(decode(&encode(bytes)).as_deref(), Ok(bytes), "length {len}");
        }
    }

    #[test]
    fn invalid_input_is_refused() {
        for bad in [
            "Zg",
            "Zg=",
            "Z===",
            "Zm9v!A==",
            "Zm9vYg==Zg==",
            "=Zm9",
            "Zh==",
        ] {
            assert!(decode(bad).is_err(), "{bad:?} was accepted");
        }
    }
}
