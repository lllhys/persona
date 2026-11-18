import SwiftUI

// The pictures in a reply (#9474, docs/design/DESIGN_SYSTEM.md "Pictures in replies"; the web's are the same numbers,
// #9475): a lone picture's height, how wide a run gets, the gap between squares and a failed picture's tile. A file of
// their own, away from Theme.swift; Shared/, so the watch and the widgets compile them, though they draw no replies.

nonisolated extension Theme.Size {
    /// A lone picture's height: its box before it arrives too, so only its width changes when it does.
    static let replyPicture: CGFloat = box240
    /// The widest a run of pictures gets: two squares of `replyPicture` side by side (an iPad's column stops here).
    static let replyPictures: CGFloat = box240 * 2
    /// A picture that did not load: a tile this size with the retry mark, not a picture-sized gap.
    static let replyPictureFailed: CGFloat = box80
}

nonisolated extension Theme.Space {
    /// Between the squares of a run of pictures.
    static let replyPictureGap: CGFloat = xs
}

nonisolated extension Theme.Fill {
    /// Over the last square of a run, the one that counts the pictures not drawn: dark enough for white words on a
    /// bright photo in both modes (the viewed images' badge uses the same).
    static let morePictures = Color.black.opacity(0.45)
}

nonisolated extension Theme {
    /// A reply's pictures' numbers that are not sizes.
    enum ReplyPicture {
        /// A picture inside a line of text: this many times the body's size tall, the line's height.
        static let inlineHeight: CGFloat = 1.2
    }
}
