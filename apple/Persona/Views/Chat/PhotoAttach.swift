import PhotosUI
import SwiftUI

// Photos sent with a message: an agent's composer (`Composer`) and New Agent's first message (#2321). Each is scaled
// down to 1600 px on its long side and sent as JPEG, at most four at a time. Videos are the composer's own
// (VideoAttach.swift): they go to the computer as files.

@MainActor enum MessagePhotos {
    static let limit = 4

    /// A picture as a message carries it; nil when it can't be encoded.
    static func payload(_ img: UIImage) -> (UIImage, ImagePayload)? {
        let scaled = img.scaledDown(maxSide: 1600)
        guard let jpeg = scaled.jpegData(compressionQuality: 0.75) else { return nil }
        return (scaled, ImagePayload(mime: "image/jpeg", data: jpeg.base64EncodedString()))
    }

    /// img after images, unless there are `limit` already (a toast says so).
    static func add(_ img: UIImage, to images: inout [(UIImage, ImagePayload)], model: AppModel) {
        guard images.count < limit else {
            model.toast = Toast(String(localized: "Up to 4 at a time"), style: .error)
            return
        }
        if let p = payload(img) {
            images.append(p)
            Haptics.tap()
        }
    }

    /// Reads the picked pictures in order and hands each to add (iCloud-only ones are downloaded here, HEIC and the
    /// like re-encoded by `payload`); a toast says how many couldn't be read.
    static func load(_ items: [PhotosPickerItem], model: AppModel, add: (UIImage) -> Void) async {
        var failed = 0
        for item in items {
            guard let data = try? await item.loadTransferable(type: Data.self), let img = UIImage(data: data) else {
                failed += 1
                continue
            }
            add(img)
        }
        if failed > 0 {
            model.toast = Toast(String(localized: "Couldn't read \(failed) photos (they may still be downloading from iCloud) [Chat View 5712c0]"), style: .error)
        }
    }
}
