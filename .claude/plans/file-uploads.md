# File uploads to a machine

Ivan's decision (2026-10-29): uploads get a plan of their own, to approve before any work starts.
It came out of commit 2 of the nightly-repairs plan (landed; `phases.md` row "Nightly repairs"), whose upload
speed reading had nothing to measure.

## What is missing

- The browser never sends file data: `SessionChannel` has no way to queue a file piece, and the
  file manager only lists folders and downloads.
- The agent answers `FileUploadRequest` by dropping it (`handle_upload` in
  `agent/crates/mesh-agent-core/src/session/handlers/file.rs` writes nothing).
- The session's `file_write` permission exists in the protocol and the server, and nothing uses it.

RMM example: a technician needs to push a 400 MB vendor installer to the front-desk PC of a dental
clinic. OpenGate cannot do it on any path; MeshCentral and RustDesk both can.

## The feature

1. The file manager gains an upload: the technician picks a local file and a folder on the
   machine; the browser sends `FileUploadRequest` (path, size), then the file in 256 KiB pieces on
   the files lane, which rides the files channel over Direct and the relay otherwise.
2. The agent accepts it only with `file_write`, writes to a temporary name beside the target, and
   renames once every byte arrived; a dropped session leaves no partial file under the real name.
   It reports progress, completion and refusal (no permission, no space, path outside what the
   session may write).
3. Uploads use the same flow control downloads do: the agent's receiving side holds at most two
   pieces before the browser's sending side waits.

## Speed reading (from the parent plan)

- In an upload Chrome's SCTP sends, and it resends on the third report and halves its window with
  no time rule (dcSCTP `outstanding_data.cc`, `retransmission_queue.cc`), so the patched agent does
  not help an upload on a reordering line.
- The speed spec gains an upload over Direct at 100 Mbit/s with 1% of packets out of order, the
  whole 512 MiB file to completion, under a 6-minute limit of its own, marked "read" with no
  floor; [`Remote-Sessions.md`](../../docs/product/Remote-Sessions.md) states the behaviour.

## Tests first

- Agent: an upload without `file_write` is refused and writes nothing; a session ended mid-upload
  leaves no file under the target name; a whole upload matches its checksum.
- Browser: the upload's pieces queue on the files lane and each fits the bulk frame cap.
- End to end: an upload over the relay and over Direct, checksum whole on the machine.

## Open questions for Ivan

- Where a technician may write: anywhere the agent's account can, or only a set of folders.
- Whether an existing file is replaced, refused, or renamed.
