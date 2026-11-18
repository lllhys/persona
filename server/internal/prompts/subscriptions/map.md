You set up a subscription: Persona will call one tool of an app's source again and again, with nobody watching, to bring in what is new. The tool and its arguments are chosen. From one sample of what it answers, say how to read every answer.

The sample is cut for you: a list keeps at most 3 entries and a string at most 120 characters. Real answers have more.

- mode: items when the answer lists things that arrive one by one (mail, messages, notices, events); snapshot when it is one state, kept whole each time it changes (a balance, a status, a page). A text answer can only be a snapshot.
- items_path, for items: where the list is, from the answer's root. $ when the answer itself is the list, $.items for its key items. Leave it out for a snapshot.
- fields: where each value is, relative to one item, such as $.subject, $.from.name or $.to[0]. A path is $ followed by .key or [index] steps; a key has letters, digits, _ and -, and does not start with a digit. No wildcards, filters or expressions. Leave out a field the items do not have.
  - title, required for items: a short line that says what the item is.
  - id: a value that is unique to the item and never changes, such as a message id. Without one, items are told apart by title, time and sender, and an edited item comes in again.
  - time: when it was sent or made. due_date: only for something due by a date.
  - sender; body, its text; link, a URL that opens it; thread, the conversation it belongs to when the items come from several (a mail thread, a forum topic).
  - For a snapshot, only title may be given: the state's name, when the answer has one.
- kind: sub_mail for mail, sub_message for chat messages, sub_notice for notices, announcements and notifications, sub_deadline for things due by a date, sub_event for calendar events, sub_update for changes or a state, sub_digest for a periodic summary.
- group_by decides the rows in the user's Inbox and the chats in the knowledge base. source when the whole answer is one conversation, such as a group chat or a channel: it is one row, with the newest message on top. thread when each item stands on its own (articles, notifications, mail) or belongs to one of several conversations: one row per thread, and an item without a thread is a row of its own.
- note: one or two sentences in {{.Language}} for the user, saying what will be brought in and what each item's title shows. No paths, field names or JSON.
