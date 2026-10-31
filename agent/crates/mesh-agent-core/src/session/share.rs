//! The turn between the link's channels: typing first, then whichever of screen and files has sent
//! fewer bytes, a frame at a time; a side with nothing waiting banks no credit while it idles.

/// The two bulk channels' byte counts, screen first, and which had nothing waiting at the last
/// pick.
#[derive(Debug, Default)]
pub(crate) struct Share {
    sent: [u64; 2],
    idle: [bool; 2],
}

/// What the link's writer sends next.
#[derive(Debug, PartialEq, Eq)]
pub(crate) enum Next {
    /// The typing channel's waiting piece.
    Typing,
    /// The screen (0) or files (1) channel's waiting piece.
    Bulk(usize),
}

impl Share {
    /// Which of screen (0) and files (1) goes next, given which has something waiting; the
    /// screen goes first on a tie.
    pub(crate) fn pick(&mut self, waiting: [bool; 2]) -> Option<usize> {
        self.idle = waiting.map(|w| !w);
        match waiting {
            [true, true] => Some(usize::from(self.sent[1] < self.sent[0])),
            [true, false] => Some(0),
            [false, true] => Some(1),
            [false, false] => None,
        }
    }

    /// Counts `bytes` sent on `slot`; a side idle at the last pick keeps level with it.
    pub(crate) fn sent(&mut self, slot: usize, bytes: usize) {
        self.sent[slot] += bytes as u64;
        let other = 1 - slot;
        if self.idle[other] {
            self.sent[other] = self.sent[other].max(self.sent[slot]);
        }
    }

    /// What goes on the link now: typing first, at once while it has nothing unconfirmed, else once
    /// every channel's unconfirmed bytes and its piece fit `limit`; then [`Share::send_now`]'s pick.
    pub(crate) fn link_next(
        &mut self,
        typing: Option<usize>,
        typing_outstanding: usize,
        bulk: [Option<usize>; 2],
        outstanding: usize,
        limit: usize,
    ) -> Option<Next> {
        if let Some(len) = typing {
            return (typing_outstanding == 0 || outstanding + len <= limit).then_some(Next::Typing);
        }
        self.send_now(bulk, outstanding, limit).map(Next::Bulk)
    }

    /// Which waiting piece, by length, goes on the link now: the one [`Share::pick`] chooses, while
    /// both channels' unconfirmed bytes and that piece fit `limit`; an idle path always takes one.
    pub(crate) fn send_now(
        &mut self,
        waiting: [Option<usize>; 2],
        outstanding: usize,
        limit: usize,
    ) -> Option<usize> {
        let slot = self.pick(waiting.map(|w| w.is_some()))?;
        let len = waiting[slot]?;
        if outstanding > 0 && outstanding + len > limit {
            return None;
        }
        self.sent(slot, len);
        Some(slot)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use mesh_protocol::direct::MAX_PIECE;
    use std::collections::VecDeque;

    #[test]
    fn the_channel_with_fewer_bytes_sent_goes_next() {
        let mut share = Share::default();
        assert_eq!(share.pick([true, true]), Some(0));
        share.sent(0, 100);
        assert_eq!(share.pick([true, true]), Some(1));
        share.sent(1, 300);
        assert_eq!(share.pick([true, true]), Some(0));
        assert_eq!(share.pick([false, false]), None);
    }

    #[test]
    fn a_channel_with_nothing_waiting_banks_no_credit() {
        let mut share = Share::default();
        for _ in 0..5 {
            assert_eq!(share.pick([false, true]), Some(1));
            share.sent(1, 16 * 1024);
        }
        assert_eq!(share.pick([true, true]), Some(0));
        share.sent(0, 16 * 1024);
        assert_eq!(
            share.pick([true, true]),
            Some(1),
            "the screen takes one turn, never the five the files took alone"
        );
    }

    #[test]
    fn both_channels_share_by_bytes_and_stay_together_under_the_limit() {
        const LIMIT: usize = 128 * 1024;
        // The screen sends 4 KiB pictures and the files 16 KiB pieces, both always waiting.
        let piece = [4 * 1024, MAX_PIECE];
        let mut share = Share::default();
        let mut in_flight: VecDeque<usize> = VecDeque::new();
        let (mut outstanding, mut sent) = (0usize, [0usize; 2]);
        let mut peak = 0;
        for ms in 0..20_000 {
            // The fake link frees this many bytes a millisecond, slowing eightfold halfway.
            let mut freed = if ms < 10_000 { 1_200 } else { 150 };
            while let Some(front) = in_flight.front_mut() {
                let take = freed.min(*front);
                *front -= take;
                outstanding -= take;
                freed -= take;
                if *front > 0 {
                    break;
                }
                in_flight.pop_front();
            }
            while let Some(slot) = share.send_now(piece.map(Some), outstanding, LIMIT) {
                in_flight.push_back(piece[slot]);
                outstanding += piece[slot];
                sent[slot] += piece[slot];
                peak = peak.max(outstanding);
            }
            assert!(
                sent[0].abs_diff(sent[1]) <= MAX_PIECE,
                "at {ms} ms the screen sent {} and the files {}",
                sent[0],
                sent[1]
            );
        }
        assert!(peak <= LIMIT, "the two channels held {peak} bytes together");
        assert!(sent[0] > 1024 * 1024, "both channels kept sending");
    }

    #[test]
    fn typing_goes_first_and_counts_under_the_one_limit() {
        const LIMIT: usize = 128 * 1024;
        // A listing pages in on typing while a download keeps the files channel full.
        let mut share = Share::default();
        let mut in_flight: VecDeque<(usize, usize)> = VecDeque::new();
        let (mut outstanding, mut typing_out, mut sent) = (0usize, 0usize, [0usize; 2]);
        let (mut listing, mut peak) = (64usize, 0usize);
        for _ in 0..2_000 {
            let mut freed = 1_200;
            while let Some((typing, front)) = in_flight.front_mut() {
                let take = freed.min(*front);
                *front -= take;
                outstanding -= take;
                if *typing == 1 {
                    typing_out -= take;
                }
                freed -= take;
                if *front > 0 {
                    break;
                }
                in_flight.pop_front();
            }
            let files = [None, Some(MAX_PIECE)];
            while let Some(next) = share.link_next(
                (listing > 0).then_some(MAX_PIECE),
                typing_out,
                files,
                outstanding,
                LIMIT,
            ) {
                match next {
                    Next::Typing => {
                        listing -= 1;
                        typing_out += MAX_PIECE;
                        in_flight.push_back((1, MAX_PIECE));
                    }
                    Next::Bulk(slot) => {
                        sent[slot] += MAX_PIECE;
                        in_flight.push_back((0, MAX_PIECE));
                    }
                }
                outstanding += MAX_PIECE;
                peak = peak.max(outstanding);
            }
            if listing > 0 {
                assert_eq!(sent[1], 0, "no file piece goes while a listing page waits");
            }
        }
        assert_eq!(listing, 0, "the listing paged in");
        assert!(sent[1] > 1024 * 1024, "the download ran once it had");
        assert!(
            peak <= LIMIT,
            "the three channels held {peak} bytes together"
        );
    }

    #[test]
    fn a_keystroke_goes_at_once_while_typing_has_nothing_unconfirmed() {
        let mut share = Share::default();
        let full = 128 * 1024;
        assert_eq!(
            share.link_next(Some(40), 0, [None, Some(MAX_PIECE)], full, full),
            Some(Next::Typing)
        );
        assert_eq!(
            share.link_next(Some(40), 40, [None, Some(MAX_PIECE)], full, full),
            None,
            "behind its own unconfirmed bytes, typing waits for room and the files wait behind it"
        );
    }

    #[test]
    fn an_idle_path_takes_a_piece_larger_than_the_limit() {
        let mut share = Share::default();
        assert_eq!(
            share.send_now([None, Some(64 * 1024)], 0, 16 * 1024),
            Some(1)
        );
        assert_eq!(share.send_now([None, Some(1)], 16 * 1024, 16 * 1024), None);
    }
}
