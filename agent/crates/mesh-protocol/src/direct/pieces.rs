//! Frames cut into link pieces and put back together; a frame's own length header marks its end.

use crate::codec::{
    FRAME_CONTROL, FRAME_DESKTOP, FRAME_FILE, FRAME_PING, FRAME_PONG, FRAME_TERMINAL,
};

/// Largest piece sent on the link (RFC 8831 §6.6, for an association without message
/// interleaving).
pub const MAX_PIECE: usize = 16 * 1024;

/// Largest whole frame the link reassembles: the relay's message limit, so both paths carry the
/// same frames.
pub const MAX_LINK_FRAME: usize = 4 * 1024 * 1024;

/// Bytes before a frame's payload: one type byte and a four-byte length.
const HEADER: usize = 5;

/// Why a piece could not be put back into a frame.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
#[non_exhaustive]
pub enum ReassemblyError {
    /// The header announces a frame past [`MAX_LINK_FRAME`].
    #[error("a link frame of {0} bytes is past the {MAX_LINK_FRAME}-byte limit")]
    TooLarge(usize),
    /// The first byte names no frame type.
    #[error("unknown frame type 0x{0:02x} on the link")]
    UnknownType(u8),
}

/// Cuts an encoded frame into pieces of at most [`MAX_PIECE`] bytes, sent back to back.
pub fn split(frame: &[u8]) -> impl Iterator<Item = &[u8]> {
    frame.chunks(MAX_PIECE)
}

/// Puts pieces back into whole frames, holding at most one frame in part.
#[derive(Debug, Default)]
pub struct Reassembler {
    buf: Vec<u8>,
}

impl Reassembler {
    /// Returns an empty reassembler.
    pub fn new() -> Self {
        Self::default()
    }

    /// Appends one piece and returns every frame it completes, in order.
    pub fn push(&mut self, piece: &[u8]) -> Result<Vec<Vec<u8>>, ReassemblyError> {
        self.buf.extend_from_slice(piece);
        let mut frames = Vec::new();
        while let Some(len) = whole_frame_len(&self.buf)? {
            let rest = self.buf.split_off(len);
            frames.push(std::mem::replace(&mut self.buf, rest));
        }
        Ok(frames)
    }

    /// Discards a frame received in part, which its sender re-sends whole.
    pub fn reset(&mut self) {
        self.buf = Vec::new();
    }

    /// Bytes held of a frame not yet complete.
    pub fn pending(&self) -> usize {
        self.buf.len()
    }
}

/// The length of the frame at the start of `buf` once it is all there, or `None` while it is not.
fn whole_frame_len(buf: &[u8]) -> Result<Option<usize>, ReassemblyError> {
    let Some(&kind) = buf.first() else {
        return Ok(None);
    };
    match kind {
        FRAME_PING | FRAME_PONG => Ok(Some(1)),
        FRAME_CONTROL | FRAME_DESKTOP | FRAME_TERMINAL | FRAME_FILE => {
            if buf.len() < HEADER {
                return Ok(None);
            }
            let payload = u32::from_be_bytes([buf[1], buf[2], buf[3], buf[4]]) as usize;
            let total = HEADER + payload;
            if total > MAX_LINK_FRAME {
                return Err(ReassemblyError::TooLarge(total));
            }
            Ok((buf.len() >= total).then_some(total))
        }
        other => Err(ReassemblyError::UnknownType(other)),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{Frame, TerminalFrame};

    fn terminal(len: usize) -> Vec<u8> {
        Frame::Terminal(TerminalFrame { data: vec![7; len] })
            .encode()
            .unwrap()
    }

    #[test]
    fn a_frame_is_cut_into_pieces_no_larger_than_the_limit() {
        let frame = terminal(40_000);
        let pieces: Vec<&[u8]> = split(&frame).collect();
        assert_eq!(pieces.len(), 3);
        assert!(pieces.iter().all(|p| p.len() <= MAX_PIECE));
        assert_eq!(pieces.concat(), frame);
    }

    #[test]
    fn pieces_come_back_as_the_frames_they_were_cut_from() {
        let first = terminal(40_000);
        let second = terminal(10);
        let mut r = Reassembler::new();
        let mut out = Vec::new();
        for piece in split(&first).chain(split(&second)) {
            out.extend(r.push(piece).unwrap());
        }
        assert_eq!(out, vec![first, second]);
        assert_eq!(r.pending(), 0);
    }

    #[test]
    fn a_frame_completes_only_with_its_last_piece() {
        let frame = terminal(40_000);
        let pieces: Vec<&[u8]> = split(&frame).collect();
        let mut r = Reassembler::new();
        assert!(r.push(pieces[0]).unwrap().is_empty());
        assert!(r.push(&pieces[1][..1]).unwrap().is_empty());
        assert!(r.push(&pieces[1][1..]).unwrap().is_empty());
        assert_eq!(r.push(pieces[2]).unwrap(), vec![frame]);
    }

    #[test]
    fn ping_and_pong_are_whole_in_one_byte() {
        let mut r = Reassembler::new();
        assert_eq!(
            r.push(&[FRAME_PING, FRAME_PONG]).unwrap(),
            vec![vec![FRAME_PING], vec![FRAME_PONG]]
        );
    }

    #[test]
    fn a_frame_past_the_relay_limit_is_refused_from_its_header() {
        let mut r = Reassembler::new();
        let announced = (MAX_LINK_FRAME - HEADER + 1) as u32;
        let mut header = vec![FRAME_FILE];
        header.extend_from_slice(&announced.to_be_bytes());
        assert_eq!(
            r.push(&header),
            Err(ReassemblyError::TooLarge(MAX_LINK_FRAME + 1))
        );

        let mut largest = vec![FRAME_FILE];
        largest.extend_from_slice(&((MAX_LINK_FRAME - HEADER) as u32).to_be_bytes());
        let mut fresh = Reassembler::new();
        assert!(
            fresh.push(&largest).unwrap().is_empty(),
            "4 MiB itself is allowed"
        );
    }

    #[test]
    fn an_unknown_type_byte_is_refused() {
        let mut r = Reassembler::new();
        assert_eq!(
            r.push(&[0x7f, 0, 0]),
            Err(ReassemblyError::UnknownType(0x7f))
        );
    }

    #[test]
    fn reset_discards_a_frame_held_in_part() {
        let frame = terminal(40_000);
        let mut r = Reassembler::new();
        r.push(&frame[..100]).unwrap();
        assert_eq!(r.pending(), 100);
        r.reset();
        assert_eq!(r.pending(), 0);
        assert_eq!(r.push(&frame).unwrap(), vec![frame]);
    }
}
