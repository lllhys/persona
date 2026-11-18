import PhotosUI
import SwiftUI
import UniformTypeIdentifiers

/// Where New from Photo's image comes from (#2405): Photos (`PhotosPicker`) or an image file (the Mac's open panel, the
/// Files app), opened as the crop sheet (`AvatarCropView`). Hung on the avatar picker (`PersonaPicker`), which says which
/// one to open.
struct AvatarSources: ViewModifier {
    var uploads: AvatarUploadsModel
    @Binding var choosingPhoto: Bool
    @Binding var choosingFile: Bool
    @Binding var cropping: AvatarUploadModel?
    @State private var photo: PhotosPickerItem?

    func body(content: Content) -> some View {
        content
            .photosPicker(isPresented: $choosingPhoto, selection: $photo, matching: .images)
            .onChange(of: photo) { _, item in
                guard let item else { return }
                photo = nil
                Task { await open(item) }
            }
            .fileImporter(isPresented: $choosingFile, allowedContentTypes: [.image]) { result in
                Task { await open(result) }
            }
            .sheet(item: $cropping) { AvatarCropView(model: $0) }
    }

    /// The photo picked in Photos, as a crop sheet.
    private func open(_ item: PhotosPickerItem) async {
        do {
            guard let data = try await item.loadTransferable(type: Data.self) else { return uploads.failedToRead(nil) }
            cropping = await uploads.picked(data)
        } catch {
            uploads.failedToRead(error)
        }
    }

    /// The image file picked, as a crop sheet.
    private func open(_ result: Result<URL, any Error>) async {
        do {
            let url = try result.get()
            let data = try await Task.detached(priority: .userInitiated) { try AvatarImage.readPickedFile(url) }.value
            cropping = await uploads.picked(data)
        } catch let e as CocoaError where e.code == .userCancelled {
            return
        } catch {
            uploads.failedToRead(error)
        }
    }
}
