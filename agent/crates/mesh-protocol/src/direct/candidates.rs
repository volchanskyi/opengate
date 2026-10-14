//! The host's bound on the remote candidates it checks: how many, and never a privileged port.

/// Most remote candidates the host accepts for one link, trickled and offered together.
pub const MAX_REMOTE_CANDIDATES: usize = 32;

/// Lowest port a remote candidate may name; the ports below belong to system services.
pub const LOWEST_CANDIDATE_PORT: u16 = 1024;

/// Admits remote candidates for one link within the bound. It limits how many addresses the host
/// checks, not which ones.
#[derive(Debug, Default)]
pub struct CandidateFilter {
    admitted: usize,
}

impl CandidateFilter {
    /// Returns a filter that has admitted nothing.
    pub fn new() -> Self {
        Self::default()
    }

    /// Whether `candidate` may be checked, counting it when admitted. The value is a candidate
    /// attribute, with or without its `a=` prefix: `candidate:<f> <c> <t> <p> <addr> <port> ...`.
    pub fn admit(&mut self, candidate: &str) -> bool {
        if self.admitted >= MAX_REMOTE_CANDIDATES {
            return false;
        }
        let Some(port) = candidate_port(candidate) else {
            return false;
        };
        if port < LOWEST_CANDIDATE_PORT {
            return false;
        }
        self.admitted += 1;
        true
    }

    /// Returns `sdp` with every candidate line the filter does not admit removed.
    pub fn filter_sdp(&mut self, sdp: &str) -> String {
        let mut out = String::with_capacity(sdp.len());
        for line in sdp.split_inclusive('\n') {
            let bare = line.trim_end_matches(['\r', '\n']);
            if bare.starts_with("a=candidate:") && !self.admit(bare) {
                continue;
            }
            out.push_str(line);
        }
        out
    }

    /// How many candidates the filter has admitted.
    pub fn admitted(&self) -> usize {
        self.admitted
    }
}

/// The port field of a candidate attribute, or `None` when the line is not one.
fn candidate_port(candidate: &str) -> Option<u16> {
    let body = candidate.strip_prefix("a=").unwrap_or(candidate);
    let fields = body.strip_prefix("candidate:")?;
    fields.split_ascii_whitespace().nth(5)?.parse().ok()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn line(port: u16) -> String {
        format!("candidate:1 1 udp 2122260223 192.0.2.10 {port} typ host generation 0")
    }

    #[test]
    fn a_candidate_on_an_ordinary_port_is_admitted() {
        let mut f = CandidateFilter::new();
        assert!(f.admit(&line(50_000)));
        assert!(f.admit(&format!("a={}", line(1024))));
        assert_eq!(f.admitted(), 2);
    }

    #[test]
    fn a_candidate_naming_a_privileged_port_is_ignored() {
        let mut f = CandidateFilter::new();
        assert!(!f.admit(&line(22)));
        assert!(!f.admit(&line(1023)));
        assert_eq!(
            f.admitted(),
            0,
            "an ignored candidate uses none of the allowance"
        );
    }

    #[test]
    fn past_thirty_two_candidates_the_rest_are_ignored() {
        let mut f = CandidateFilter::new();
        for i in 0..MAX_REMOTE_CANDIDATES {
            assert!(f.admit(&line(40_000 + i as u16)));
        }
        assert!(!f.admit(&line(50_000)));
    }

    #[test]
    fn a_line_that_is_not_a_candidate_is_ignored() {
        let mut f = CandidateFilter::new();
        for bad in [
            "",
            "candidate:",
            "candidate:1 1 udp 1 192.0.2.1",
            "candidate:1 1 udp 1 h port typ host",
        ] {
            assert!(!f.admit(bad), "{bad:?}");
        }
    }

    #[test]
    fn the_offer_keeps_only_the_candidate_lines_it_admits() {
        let sdp = format!(
            "v=0\r\nm=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\na=mid:0\r\n\
             a={}\r\na={}\r\na=sctp-port:5000\r\n",
            line(50_000),
            line(53)
        );
        let mut f = CandidateFilter::new();
        let kept = f.filter_sdp(&sdp);
        assert!(kept.contains(&line(50_000)));
        assert!(
            !kept.contains(" 53 typ"),
            "a candidate on port 53 never reaches the agent"
        );
        assert!(kept.contains("a=sctp-port:5000\r\n"));
        assert!(kept.starts_with("v=0\r\nm=application"));
        assert_eq!(f.admitted(), 1);
    }

    #[test]
    fn offered_and_trickled_candidates_share_one_allowance() {
        let offer: String = (0..30)
            .map(|i| format!("a={}\r\n", line(40_000 + i)))
            .collect();
        let mut f = CandidateFilter::new();
        f.filter_sdp(&offer);
        assert!(f.admit(&line(41_000)));
        assert!(f.admit(&line(41_001)));
        assert!(!f.admit(&line(41_002)));
    }
}
