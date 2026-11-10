# Recordings in the knowledge base

Recordings feed spoken notes, meetings, and information captured on site to Persona; this is not a second recording product. Both iPhone and Apple Watch record in segments; the watch sends them to the phone via WatchConnectivity, and the phone transcribes locally.
The resulting text (timestamped segments) automatically syncs into `recordings` in the knowledge base, where the assistant and Agents can find it with `kb_search` / `kb_list` / `kb_get`. The knowledge base retains only the most recent N days (14 by default).

Since 2026-11-05 (#3235), there is no longer a "Composer + › Recording context" entry: user feedback was that this entry was wrong. Recordings should be stored in the knowledge base and retrieved by search and queries, not manually inserted into the composer each time.

Rolling recording (the watch keeps recording and only retains the last 6 hours, with health bookmarks) is a mode of this same system — see `docs/apple/recording-rolling.md`. Pulled ranges are transcribed on device and sync into Knowledge like any other recording; there is no composer "as context" path.

## Why send only text

- Audio is roughly 14 MB / hour; the knowledge base searches text, so text is enough. The assistant needs "who said what at that day's meeting," not the audio itself.
- Privacy: original audio stays on the phone (watch audio is still cleaned up after transfer to the phone) and never leaves the device; the hub holds only transcripts.
- Recording entries are separate from meetings (`meetings`): a meeting is an external item from the Lark calendar, stored as one JSON document and batch-tagged by a model; a recording has a full transcript, its own retention period, and UUID-based writes and deletes from the phone. Their lifecycles differ, and combining them complicates cleanup and display.

A recording marked as a meeting (`kind: "meeting"`, with the phone calendar's event in `calendar`) also gets structured minutes from the hub: [meeting-minutes.md](../assistant/meeting-minutes.md).

## Synchronization

See [protocol.md](../protocol/protocol.md) ("Recording synchronization") for the API and [knowledge.md](../assistant/knowledge.md) ("Recordings") for storage in the knowledge base.

- The phone uses each recording's UUID as the key for `PUT /api/v1/recordings/{id}`, sending the complete state so far each time (full replacement). `revision` increases monotonically: replayed or out-of-order uploads retain only the newest copy.
- Upload available segments before transcription finishes (`complete: false`), then upload again as transcription progresses, recording ends, or a language change triggers retranscription; once `complete: true`, the hub generates a summary and tags.
- Deleting a recording on the phone → `DELETE /api/v1/recordings/{id}`, deleting it from the knowledge base too; the hub retains a tombstone, returns 410 for late uploads, and the phone discards the pending upload.
- When offline, after hub restarts, or when the manager is disconnected (503), the phone queues retries; `GET /api/v1/recordings` reconciles state (uploading recordings absent from the hub or with an older revision there, and removing those deleted on the hub).
- Retention: the assistant setting `recording_days` (14 days by default, 1–365) can be read or changed through `assistant.recording_days` in `persona_settings`; the hub deletes expired files and index entries together, and recordings older than the retention window are no longer uploaded.
  The phone uses `days` from reconciliation to remove expired local recordings as well (audio and text together, leaving active recordings untouched): the hub is authoritative for text, and local retention never exceeds knowledge-base retention.
- The iOS implementation is `RecordingSync` (`apple/Persona/Recording/`): every `RecordingLibrary.save` triggers a round (1-second debounce); `revision` is this state's `updatedAt` (milliseconds, guaranteed to increase on save).
  Uploaded revisions, pending deletions, rejections (400), and `days` are stored in `sync.json` in the recording directory; after restart, only changed recordings are uploaded. Transient connection failures retry with exponential backoff starting at 5 seconds, up to 10 minutes; returning to the foreground or a new save triggers an immediate retry.
  `kb_disabled` / `kb_read_only` pause automatic retries without dropping uploads or pending deletions. Fix the manager configuration, then reopen the app to retry; another transcript save cannot enable knowledge. Authentication, permission and unsupported-route failures also pause. Failed reconciliation stops the pass before writes. A DELETE 404 does not confirm deletion: the remote copy stays pending, and the list explains that it has not yet been removed.
  Reconcile on startup and return to the foreground (at most once every 10 minutes). A 410 or an entry in `deleted` during reconciliation means it was deleted elsewhere (such as the web), so delete it locally too. Recordings without any text (no recognized speech) are not uploaded, except a meeting that Persona attends, which is uploaded once when it starts ([meeting-minutes.md](../assistant/meeting-minutes.md) "Attending a meeting").

Phone recordings can use the microphone on glasses or a Bluetooth headset (Bluetooth HFP); see `docs/apple/meta-glasses.md`.

## Migration sources and boundaries

Reuse Hourhand's `Shared/SegmentRecorder.swift` (segmented AAC), `HourhandWatch/TransferQueue.swift` (persistence and transfer backoff), and `Hourhand/Transcription/SpeechEngines.swift` (on-device engines and timestamps). After migration, standardize on the `Recording` prefix and remove product accounts, payments, cloud services, location, and separate recording-library navigation.

- Audio: 16 kHz mono AAC, 32 kbps, roughly 14 MB / hour; segments are 30 seconds in the foreground and 60 seconds in the background. watchOS 11 and later finalize with `AVAudioFile.close()`; watchOS 10 releases the last file reference before the callback and must not retain a local reference inside the write loop.
- Storage: Application Support/Recordings on each device, one UUID directory per recording, with atomic updates to `recording.json`. This is not a cache and must not be deleted by cache cleanup.
- Abnormal exit: retain finalized segments and delete the unfinished tail; do not automatically restart the microphone. At most the current 30–60 seconds are lost.
- Privacy: no location collection and no audio uploads; transcript text syncs into the hub's knowledge base ("Synchronization" above) and is not automatically sent to any chat, except to the session attending a meeting the user chose to attend (#2981). Transcription uses only Apple's on-device models, with no implicit fallback to Apple servers or cloud APIs.
- The initial version has no recording summaries, speaker identification, route maps, payments, or cloud services. The user explicitly selects the recording language.

## Incremental commits

1. Shared recording format, segmentation engine, persistence, and recovery after abnormal exits.
2. On-device transcription, timestamps, resumability, and context generation.
3. Watch recording, persistent transfer, end messages, and cleanup after acknowledgment.
4. Phone recording page, preview / deletion, screenshots, and regression verification.
5. Knowledge-base synchronization (#3235): hub `recordings` and APIs, retention, web / Android knowledge pages; remove the iOS composer entry and switch to automatic sync.

## Meeting recordings

In-person meetings and hallway discussions went unrecorded: the recorder sat four taps deep (Knowledge › Recordings, the last kind chip), and nobody remembered it in the moment. A meeting recording starts in one step from wherever the user is:

| Where | How | Code |
| --- | --- | --- |
| The drawer | **Meeting Recording**, first among the places: starts recording and opens Recordings; while a recording runs the row reads "Recording… Open" and opens it | `AssistantSidebar.meetingRow`, `ShellView.startMeeting` |
| Recordings page | **Meeting Recording** (prominent) above the language picker; the plain **Start Recording** stays for notes | `RecordingCaptureControls.meetingStart` |
| Home screen icon's menu | **Record a Meeting** | `QuickAction.meetingRecording` |
| Control Center, Lock Screen, Action button | the **Record a Meeting** control, without opening the app | `MeetingRecordingControl` → `StartMeetingRecordingIntent` |
| Siri, Shortcuts, Spotlight | "Record a meeting with Persona" and its zh-Hans / ja phrases | `PersonaShortcuts`, `StartMeetingRecordingIntent` |

- `StartMeetingRecordingIntent` is an `AudioRecordingIntent`: the system lets it open the microphone in the background, on condition that a Live Activity shows for as long as the recording runs (`RecordingActivity` starts one before `perform` returns). Before the microphone was ever allowed it continues in the foreground (`continueInForeground`) so the system can ask.
- **The calendar names it** (`MeetingCalendar`): the event on now (all-day, declined and untitled ones aside; one starting within 5 minutes counts; of overlapping ones, the one that started last) gives the title, and a copy of the event goes into `RecordingEntry.meeting` (`RecordingMeeting`: event id, title, start / end, location, attendees by name). It reads only with full access already granted, so a start never waits on a prompt; a meeting recording started in the app asks once, after the recording has begun, and renames it if an event turns up. Without an event the title is "Meeting · <date>".
- A watch recording made during a calendar event is that meeting's too: the phone tags it when its first segment or end arrives (`MeetingRecorder.tagWatchRecording`, by the watch's `createdAt`). The watch has no calendar.
- Language: every start uses the one last picked on the Recordings page (`RecordingDefaults.localeKey`), Chinese until then. The on-device Chinese model writes the English words said among Chinese in Latin letters; there is no mixed-language model on device (the hub's realtime service is #2083).
- **Live Activity** (`docs/apple/live-activity.md`, "Recording"): any recording on the phone, a meeting's or a note, shows its name, what it is, bookmarks and the time on the Lock Screen and in the Dynamic Island, with **Bookmark** and **Stop** (they work locked, as a voice memo's). Tapping it opens Recordings (`persona://record`).
- **A call**: the interruption pauses the recording and closes the open segment at once (`RecordingSegmentWriter.cut`), so an app stopped during a long call keeps everything said before it. When the system ends the interruption the recording resumes by itself (whether or not it says `shouldResume`: the meeting goes on). If the microphone can't be opened again in the background, the phone posts "Recording Paused" (`MeetingRecorder.notifyPaused`), the activity says "Paused by a call · Open Persona to continue", and coming back to the app resumes it (`resumeAfterInterruption`, on `didBecomeActive`). A media services reset while paused stops the recording (its engine is gone) and keeps what was saved.
- **Stopped in the background** (the activity's Stop with the phone locked): the audio session ends, and with it the app's right to run. `MeetingRecorder.finishInBackground` holds a background task for up to 30 s, until the last segment is transcribed and uploaded, so the minutes don't wait for the next launch.

## Apple Watch

The watch records a meeting in one step too (#2720), and shows its minutes once the phone has them.

- **Starting**: **Meeting Recording** on the home list, right above Record (`WatchMeetingEntries`): one tap opens the recording page and starts at once (`WatchRecordingView(startsMeeting:)`); while something records, the card opens it. The **Meeting Recording** complication (watch face corner / circular / rectangular / inline, and the Smart Stack; `MeetingRecordingComplication`) opens the app straight into it (`persona://record?meeting=1`). The recording page also has Meeting Recording above Start Recording, and the unpaired page has it beside Record. The Ultra's Action button isn't wired yet: it needs an App Shortcut in the watch app, with its own phrase catalog.
- **The meeting mark**: a meeting recording carries `meeting: true` in every `RecordingTransfer` (each segment and the end message); a watch with an older app sends none, which reads as a note. The phone keeps the mark when the recording arrives (`RecordingLibrary.receive`), names it after the calendar event at the recording's start (`MeetingRecorder.tagWatchRecording`; the watch reads no calendar), and uploads `kind: meeting` with the event as `calendar` when one matched. A note made on the watch during a calendar event becomes that meeting's too, as the contract allows.
- **Minutes on the watch**: the phone relays a short version of its newest meetings' minutes (at most 8 meetings from the last week; 4 points, 4 decisions, 4 action items each, 120 characters an item; `WatchMinutes`) in the WatchConnectivity application context, and the watch shows them under **Meeting Minutes** (`WatchMinutesList`, `WatchMinutesView`): ready, "Writing the minutes…", or failed. The watch never talks to the hub for them. `MinutesRelay` reads them from the hub when the phone app comes to the front (again every 3 minutes while it stays there and some are still being written) and whenever a recording's page loads them, so minutes written while the phone app is closed reach the watch the next time it is opened (the hub's "minutes ready" notification is the usual reason to open it).
- **Long recordings**: the watch app records with the `audio` background mode, so recording goes on with the wrist down and the screen off; an hour is about 14 MB (32 kbps AAC). Segments are 30 s while the phone is reachable and 60 s otherwise; the watch sends them in batches every 3 minutes (5 in Low Power Mode) and keeps each until the phone confirms it is saved. It stops by itself, keeping what was recorded, under 5 % battery off the charger or under 150 MB of free space. A call or Siri pauses it like the phone's (the open segment is closed at once) and it resumes when they end. Battery use is not measured yet: continuous microphone capture, AAC encoding and the transfers are the costs; check it on a real watch with a one-hour recording.
- **Verified in the simulator**: the watch app builds; screenshots of the home entry, a meeting being recorded and the short minutes (`-showMeetingSample recording|minutes`, `-openWatchItem minutes|minutes-writing`). The real watch checks are in the device checklist: wrist down for an hour, the screen off, the phone out of range and back, and minutes reaching the watch.

## Minutes

The hub writes minutes for a meeting recording once its text is complete (#2710; the hub side and the field contract are `docs/assistant/meeting-minutes.md`). Every upload says `kind: "meeting"` or `"note"`, and a meeting the device matched to a calendar event adds the event as a hint, `calendar: {title, attendees, location}` (no event id: the hub can't see the phone's calendar). `RecordingUpload` is the one place that writes them. A recording started from **Start Recording** stays a note; a watch recording becomes a meeting only when the phone matches it to an event.

- The recording's page shows them above the transcript (`RecordingMinutesSections`): the title when it differs, when and where, the attendees, then key points (who said it, when known), decisions, action items (who, by when, as said) and open questions. Each item carries `at`, seconds into the recording; tapping the time scrolls the transcript to that line and highlights it for two seconds (`RecordingMinutes.sentenceIndex`: the last line that started by then).
- **Copy** and **Share** send them as Markdown rendered on the phone (`RecordingMinutes.text`), every item with its time.
- Until they exist the section says what they wait for: the recording to end and be transcribed, the text to reach the hub, or "Writing the minutes…" while `detail.meta.minutes_state` is `pending` (asked again every 15 s while the page is open: `RecordingMinutesModel`). `failed` offers **Try Again** (`POST /api/v1/recordings/{id}/minutes`; a `409` means the hub doesn't have the complete transcript yet). A note shows minutes only if the hub wrote some.
- Read from `GET /api/v1/kb/recordings/{id}`: `detail.minutes` (`title`, `when`, `where`, `attendees`, `points`, `decisions`, `actions` with `who` / `what` / `by`, `open`) and `detail.meta.minutes_state`. Sections a hub leaves out are empty.
- Mock hub: `MOCK_RECORDING_MINUTES=ready|pending|failed` (`scripts/mock/mock_hub.py`; `POST /mock/recording_minutes` switches it) writes made-up minutes in that shape from a meeting's own sentences when it is stored complete, leaves them pending, or fails them.

## Entry points

- Phone: Knowledge › "Recordings" (the last type capsule; screenshot arguments `-tab assistant -openAssistantPage knowledge -showKnowledgeKind recordings`, with `-showRecordingSample YES` to add a fictional recording and `-openRecording YES` to open it).
  On the phone, this category lists local recordings (including those being recorded or transcribed), not hub copies; Mac has no recording feature and lists hub `recordings`. Existing interface styles and layout are preserved.
- Apple Watch: "Record" on the home screen; recording can start even when unpaired or the hub is offline. Start, pause / resume, bookmark, stop; watchOS 11 and later support Double Tap to start or add a bookmark.
- Phone recording section: choose Chinese / English / Japanese, then start recording; recording continues after leaving the page or locking the screen, and is stopped on return. Each list row shows source, duration, and state: Recording, Waiting for watch transfer, Transcribing, Waiting to sync, Syncing, Synced to knowledge base (Partially synced), or Not accepted by knowledge base; when the hub is unreachable, the reason appears below the list.
- Details: bookmarks and timestamped transcript; resume transcription, change the language and retranscribe (resync replaces the old version), or delete (immediate local deletion after confirmation, synchronized deletion from the knowledge base, queued offline). Original audio is not uploaded.
- Knowledge base: iOS, web, and Android knowledge pages all have a "Recordings" category. Lists show name, time, duration, and source device; details show summary, bookmarks, and full timestamped text.
- The watch cleans up transferred audio only after receiving the phone's application-level persistence acknowledgment; segments whose transfer succeeded but have not been acknowledged remain. The end message transfers independently to avoid displaying Recording forever when stopped at a segment boundary.
- iOS uses `SpeechTranscriber`, trying `DictationTranscriber` if the language is unsupported; system models download on demand. If no model is available, show the reason and retain the recording for retry, without using the cloud.
- Dictation and long recordings share one microphone ownership lock, so neither can reconfigure or close the other's audio session. Deletion retains a UUID tombstone so late watch segments cannot recreate the recording.

## Verified (from #3235 onward)

- iOS / watch simulator and Mac (`CODE_SIGNING_ALLOWED=NO`) builds; `RecordingSyncTests`: upload only once until changed, queued offline retries, eventual deletion after deleting offline, local deletion following knowledge-base deletion, reconciliation restoring recordings lost by the hub, expiry removing audio too, and no upload without text.
- End-to-end UI (`RecordingKnowledgeUITests`, `TEST_RUNNER_PERSONA_MOCK_HUB`, read back through mock hub `/api/v1/recordings` and `/mock/recordings`): a fictional recording appears in Knowledge › Recordings and automatically syncs to the hub (only text and timestamps); deleting from details also deletes it from the hub, with no later upload; no recording entry in the + menu of the new-chat composer.
  `TEST_RUNNER_PERSONA_SHOTS=<directory>` and `TEST_RUNNER_PERSONA_SHOTS_APPEARANCE=light|dark` save three screenshots: list, details, and + menu.

## Verified (composer-entry era, before #3235)

- Phone and watch simulator builds; Mac shared composer build.
- Unit tests for recording format, out-of-order and duplicate segments, abnormal-exit recovery, transcription checkpoints, timestamps, and XML context.
- End-to-end UI: selecting "Use as context" sends no message to the mock hub; tapping Send delivers timestamped text without audio.
- Phone transcript preview screenshots in dark / light mode; watch entry screenshot. A watch Picker compressed into a narrow strip was replaced with single-line language-switch buttons.

## Verification limits

Paired simulator `transferFile` does not deliver, and simulator microphones and speech models cannot replace real-device acceptance. Unit tests cover out-of-order delivery, duplicates, stopping at segment boundaries, recovery, and transcription checkpoints; phone and watch pages were screenshotted on dedicated simulators. Final real-device checks are required for continued recording while locked, incoming calls, transfer of watch offline backlogs, long recordings, model downloads, and permission denial.
