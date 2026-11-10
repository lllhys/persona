#if os(iOS)
import AVFoundation
import BeautifulUI
import Photos
import SwiftUI
import UIKit

// The composer's 「+」 on the iPhone (docs/apple/ios.md, New shell, "Attach panel"): Telegram's attachment menu. A sheet
// grows out of the button over the dimmed chat and rests a quarter of the screen down: a grid of recent photos (the
// camera first, two rows tall) under a header, and a tab bar floating over its bottom (Photos, Files, More). Pulled up it
// fills the screen and the grid scrolls; pulled down, tapped outside or closed, it shrinks back into the button. Photos
// picked turn the tab bar into a caption (the draft) and Send. Only Telegram's behavior and numbers are taken (GPL).

/// What the composer gets back once the panel has gone (`AttachSheet`).
enum AttachOutcome {
    /// Closed: what was picked goes into the composer, nothing is sent.
    case closed([String])
    /// Send: what was picked goes into the composer and the message is sent.
    case send([String])
    /// A row asked for something the panel cannot show itself (the camera, Files, a voice call): what was picked goes
    /// into the composer, and the action runs once the panel is gone.
    case then([String], () -> Void)
}

/// The panel's tabs, in the bar's order.
enum AttachTab: String, CaseIterable, Identifiable {
    case photos, files, more
    var id: String { rawValue }

    var title: String {
        switch self {
        case .photos: String(localized: "Photos")
        case .files: String(localized: "Files")
        case .more: String(localized: "More")
        }
    }

    var symbol: String {
        switch self {
        case .photos: Theme.Icon.photos
        case .files: Theme.Icon.file
        case .more: Theme.Icon.more
        }
    }
}

/// Lets the composer's rows in the panel close it before they act (a row is the composer's, the panel draws it).
@Observable
final class AttachPanel {
    /// The panel is on screen (or going).
    var shown = false
    /// Where the 「+」 button is, in the window: the panel grows out of it and goes back into it.
    var source: CGRect = .zero
    /// Set by a row: the panel animates away, then hands this back.
    fileprivate(set) var leaving: AttachOutcome?

    func close(then action: @escaping () -> Void) { leaving = .then([], action) }
}

/// The photo library as the grid needs it: access, the newest photos and videos, thumbnails and full pictures.
@Observable
final class AttachLibrary {
    enum Access { case unknown, full, limited, denied }
    private(set) var access: Access = .unknown
    private(set) var assets: [PHAsset] = []
    @ObservationIgnored private let manager = PHCachingImageManager()

    func load() async {
        var status = PHPhotoLibrary.authorizationStatus(for: .readWrite)
        if status == .notDetermined { status = await PHPhotoLibrary.requestAuthorization(for: .readWrite) }
        switch status {
        case .authorized: access = .full
        case .limited: access = .limited
        default: access = .denied; assets = []; return
        }
        fetch()
    }

    func fetch() {
        let options = PHFetchOptions()
        options.sortDescriptors = [NSSortDescriptor(key: "creationDate", ascending: false)]
        options.predicate = NSPredicate(format: "mediaType == %d || mediaType == %d", PHAssetMediaType.image.rawValue, PHAssetMediaType.video.rawValue)
        options.fetchLimit = Theme.Attach.fetchLimit
        let result = PHAsset.fetchAssets(with: options)
        var list: [PHAsset] = []
        list.reserveCapacity(result.count)
        result.enumerateObjects { asset, _, _ in list.append(asset) }
        assets = list
    }

    func thumbnail(_ asset: PHAsset, side: CGFloat) async -> UIImage? {
        let options = PHImageRequestOptions()
        options.deliveryMode = .highQualityFormat
        options.resizeMode = .fast
        options.isNetworkAccessAllowed = true
        let size = CGSize(width: side * Theme.Attach.thumbnailScale, height: side * Theme.Attach.thumbnailScale)
        return await withCheckedContinuation { c in
            manager.requestImage(for: asset, targetSize: size, contentMode: .aspectFill, options: options) { @Sendable image, _ in
                c.resume(returning: image)
            }
        }
    }

    /// The whole picture, downloaded from iCloud if it has to be.
    static func picture(_ asset: PHAsset) async -> UIImage? {
        let options = PHImageRequestOptions()
        options.isNetworkAccessAllowed = true
        options.deliveryMode = .highQualityFormat
        return await withCheckedContinuation { c in
            PHImageManager.default().requestImageDataAndOrientation(for: asset, options: options) { @Sendable data, _, _, _ in
                c.resume(returning: data.flatMap(UIImage.init(data:)))
            }
        }
    }

    /// A copy of the video in a directory of its own under the app's temporary uploads, named as in Photos, for the
    /// upload (`VideoAttachments.start(staged:)`, which deletes it). A slow-motion or edited video is a composition with
    /// no file: its original goes instead.
    static func movie(_ asset: PHAsset) async -> URL? {
        var found = await movieFile(asset, version: .current)
        if found == nil { found = await movieFile(asset, version: .original) }
        guard let source = found else { return nil }
        let name = PHAssetResource.assetResources(for: asset).first?.originalFilename ?? source.lastPathComponent
        return await Task.detached(priority: .userInitiated) { () -> URL? in
            let dir = FileManager.default.temporaryDirectory.appendingPathComponent("uploads").appendingPathComponent(UUID().uuidString)
            let copy = dir.appendingPathComponent(name)
            do {
                try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
                try FileManager.default.copyItem(at: source, to: copy)
                return copy
            } catch {
                try? FileManager.default.removeItem(at: dir)
                return nil
            }
        }.value
    }

    private static func movieFile(_ asset: PHAsset, version: PHVideoRequestOptionsVersion) async -> URL? {
        let options = PHVideoRequestOptions()
        options.isNetworkAccessAllowed = true
        options.version = version
        return await withCheckedContinuation { c in
            PHImageManager.default().requestAVAsset(forVideo: asset, options: options) { @Sendable av, _, _ in
                c.resume(returning: (av as? AVURLAsset)?.url)
            }
        }
    }

    static func asset(_ id: String) -> PHAsset? {
        PHAsset.fetchAssets(withLocalIdentifiers: [id], options: nil).firstObject
    }
}

/// The panel. The composer presents it full screen with no animation of the system's (`AttachSheetHost`); it draws its own.
struct AttachSheet<More: View>: View {
    let panel: AttachPanel
    @Binding var caption: String
    /// How many more photos the composer takes (it holds four), and videos.
    var photoRoom: Int
    var videoRoom: Int
    /// Takes a photo with the camera (nil: no camera); opens Files; opens the system's photo picker.
    var camera: (() -> Void)?
    var files: () -> Void
    var allPhotos: () -> Void
    var showsMore: Bool
    @ViewBuilder var more: () -> More
    /// Called once the panel has gone.
    var finish: (AttachOutcome) -> Void

    @Environment(\.colorScheme) private var scheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.buiTheme) private var theme
    @State private var library = AttachLibrary()
    @State private var tab: AttachTab = .photos
    @State private var picked: [String] = []
    /// 0: still the 「+」 button; 1: the sheet. The corners round off on their own, quicker.
    @State private var grown: CGFloat = 0
    @State private var cornered: CGFloat = 0
    @State private var contentShown = false
    @State private var dimmed = false
    /// How far the sheet's top is down from open (`rest` at rest), and how far it is pulled down past rest.
    @State private var offset: CGFloat?
    @State private var drop: CGFloat = 0
    @State private var dip: CGFloat = 0
    @State private var dragStart: CGFloat?
    @State private var pageAtTop = true
    @State private var going = false
    @FocusState private var captionFocused: Bool
    @State private var keyboardUp = false

    private var tabs: [AttachTab] { showsMore ? AttachTab.allCases : [.photos, .files] }
    private var surface: Color { AppStyle.current.usesBeautifulUI ? theme.colors.surface : Theme.Shell.composer }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("AttachSheet")
        #endif
        GeometryReader { outer in
            let safeTop = outer.safeAreaInsets.top
            let width = outer.size.width
            let height = outer.size.height + outer.safeAreaInsets.top + outer.safeAreaInsets.bottom
            let rest = AttachSheetPhysics.rest(width: width, height: height)
            let top = safeTop + Theme.Attach.topGap
            let resting = offset ?? rest
            ZStack(alignment: .topLeading) {
                Color.black.opacity(dimmed ? Theme.Attach.dim : 0)
                    .contentShape(.rect)
                    .onTapGesture { leave(.closed(picked)) }
                    .accessibilityLabel("Close")
                    .accessibilityAddTraits(.isButton)
                    .accessibilityIdentifier("attach-dim")
                sheet(width: width, height: height, top: top, rest: rest, offset: resting)
            }
            .ignoresSafeArea(.container)
            .onAppear { appear(rest: rest) }
            .onChange(of: panel.leaving != nil) { _, leaving in if leaving, let outcome = panel.leaving { leave(outcome) } }
            .onChange(of: captionFocused) { _, focused in if focused { settle(to: 0, velocity: 0, motion: Theme.Motion.keyboard) } }
        }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillShowNotification)) { _ in keyboardUp = true }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillHideNotification)) { _ in keyboardUp = false }
        .task { await library.load() }
    }

    // MARK: The sheet

    private func sheet(width: CGFloat, height: CGFloat, top: CGFloat, rest: CGFloat, offset: CGFloat) -> some View {
        let progress = AttachSheetPhysics.restProgress(offset: offset, rest: rest)
        let sheetTop = top + offset + drop + dip + Theme.Space.xxs * progress
        let sheetHeight = height - top - offset
        let restScale = AttachSheetPhysics.scale(offset: offset, rest: rest, width: width)
        let source = panel.source
        let sheetCenter = CGPoint(x: width / 2, y: sheetTop + sheetHeight / 2)
        let startScale = source.width > 0 ? source.width / width : 0.1
        let scale = (startScale + (1 - startScale) * grown) * restScale
        let shift = CGSize(width: (source.midX - sheetCenter.x) * (1 - grown), height: (source.midY - sheetCenter.y) * (1 - grown))
        let round = width / 2
        let shape = UnevenRoundedRectangle(
            topLeadingRadius: round + (Theme.Attach.topRadius - round) * cornered,
            bottomLeadingRadius: round + (Theme.Attach.bottomRadius - round) * cornered,
            bottomTrailingRadius: round + (Theme.Attach.bottomRadius - round) * cornered,
            topTrailingRadius: round + (Theme.Attach.topRadius - round) * cornered, style: .continuous)
        return VStack(spacing: 0) {
            header
            page(width: width, open: offset == 0)
        }
        .opacity(contentShown ? 1 : 0)
        .overlay(alignment: .bottom) { bar(width: width).opacity(contentShown ? 1 : 0) }
        .frame(width: width, height: sheetHeight)
        .background(surface, in: shape)
        .clipShape(shape)
        .scaleEffect(scale)
        .offset(shift)
        .position(x: width / 2, y: sheetTop + sheetHeight / 2)
        .gesture(AttachPan(enabled: !keyboardUp && !going, shouldBegin: { location, velocity in
            beginsDrag(onHeader: location.y >= sheetTop && location.y < sheetTop + Theme.Attach.headerHeight, velocity: velocity, open: offset == 0)
        }, changed: { travel in
            follow(travel, rest: rest)
        }, ended: { travel, velocity in
            release(travel, velocity: velocity, rest: rest, height: height)
        }))
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("attach-sheet")
    }

    private var header: some View {
        VStack(spacing: 0) {
            Capsule()
                .fill(Color.primary.opacity(scheme == .dark ? Theme.Attach.grabberOpacityDark : Theme.Attach.grabberOpacityLight))
                .frame(width: Theme.Attach.grabberWidth, height: Theme.Attach.grabberHeight)
                .padding(.top, Theme.Attach.grabberTop)
            ZStack {
                Text(tab == .photos ? String(localized: "Recents") : tab.title)
                    .font(Theme.Font.heading)
                    .contentTransition(.opacity)
                    .accessibilityAddTraits(.isHeader)
                HStack {
                    Button { leave(.closed(picked)) } label: {
                        ThemedIcon(Theme.Icon.close).font(Theme.Font.heading)
                            .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                            .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Close")
                    .accessibilityIdentifier("attach-close")
                    Spacer()
                    if tab == .photos {
                        Button { leave(.then(picked, allPhotos)) } label: {
                            Text("All Photos").font(Theme.Font.secondaryMedium).lineLimit(1)
                        }
                        .buttonStyle(.plain)
                        .foregroundStyle(Theme.accent)
                        .accessibilityIdentifier("attach-all-photos")
                    }
                }
                .padding(.horizontal, Theme.Space.m)
            }
            .frame(height: Theme.Attach.headerHeight - Theme.Attach.grabberTop - Theme.Attach.grabberHeight)
        }
        .frame(maxWidth: .infinity)
    }

    // MARK: Pages

    @ViewBuilder private func page(width: CGFloat, open: Bool) -> some View {
        ZStack(alignment: .top) {
            switch tab {
            case .photos: photos(width: width, open: open).transition(.asymmetric(insertion: .identity, removal: .opacity))
            case .files: filesPage(open: open).transition(.asymmetric(insertion: .identity, removal: .opacity))
            case .more: morePage(open: open).transition(.asymmetric(insertion: .identity, removal: .opacity))
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
    }

    @ViewBuilder private func photos(width: CGFloat, open: Bool) -> some View {
        switch library.access {
        case .unknown:
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        case .denied:
            AttachNoAccess(settings: openSettings, allPhotos: { leave(.then(picked, allPhotos)) })
        case .full, .limited:
            AttachGrid(library: library, picked: $picked, width: width, camera: camera.map { camera in { leave(.then(picked, camera)) } },
                       limited: library.access == .limited, photoRoom: photoRoom, videoRoom: videoRoom)
                .scrollDisabled(!open)
                .onScrollGeometryChange(for: Bool.self) { $0.contentOffset.y + $0.contentInsets.top <= 1 } action: { _, top in pageAtTop = top }
        }
    }

    private func filesPage(open: Bool) -> some View {
        List {
            Section {
                Button { leave(.then(picked, files)) } label: {
                    Label { Text("Browse Files").lineLimit(1) } icon: { ThemedIcon(Theme.Icon.file) }
                }
                .accessibilityIdentifier("attach-browse-files")
            } footer: {
                Text("A file goes up to this conversation's computer, and its path into your message.")
            }
        }
        .scrollDisabled(!open)
        .scrollContentBackground(.hidden)
        .contentMargins(.bottom, Theme.Attach.gridBottom, for: .scrollContent)
        .onScrollGeometryChange(for: Bool.self) { $0.contentOffset.y + $0.contentInsets.top <= 1 } action: { _, top in pageAtTop = top }
    }

    private func morePage(open: Bool) -> some View {
        List { more() }
            .scrollDisabled(!open)
            .scrollContentBackground(.hidden)
            .contentMargins(.bottom, Theme.Attach.gridBottom, for: .scrollContent)
            .onScrollGeometryChange(for: Bool.self) { $0.contentOffset.y + $0.contentInsets.top <= 1 } action: { _, top in pageAtTop = top }
    }

    // MARK: The bar

    @ViewBuilder private func bar(width: CGFloat) -> some View {
        ZStack(alignment: .bottom) {
            if picked.isEmpty {
                AttachTabBar(tabs: tabs, selected: tab, width: AttachSheetPhysics.barWidth(tabs: tabs.count, width: width), pick: pick)
                    .padding(.bottom, Theme.Attach.barInset)
                    .transition(.asymmetric(
                        insertion: .modifier(active: AttachBarAway(), identity: AttachBarAway(away: false))
                            .combined(with: .opacity.animation(Theme.Motion.attachBarBack)),
                        removal: .modifier(active: AttachBarAway(), identity: AttachBarAway(away: false))
                            .combined(with: .opacity.animation(Theme.Motion.attachBarAway))))
            } else {
                captionBar
                    .padding(.bottom, captionFocused ? Theme.Space.s : Theme.Attach.barInset)
                    .transition(.opacity.animation(Theme.Motion.attachBarBack))
            }
        }
        .animation(Theme.Motion.attachBarSwap, value: picked.isEmpty)
    }

    private var captionBar: some View {
        HStack(alignment: .bottom, spacing: Theme.Space.xs) {
            TextField("Add a caption…", text: $caption, axis: .vertical)
                .lineLimit(1...4)
                .font(Theme.Font.body)
                .focused($captionFocused)
                .padding(.horizontal, Theme.Space.l)
                .padding(.vertical, Theme.Space.s)
                .scaledFrame(minHeight: Theme.Size.tapTarget)
                .modifier(ComposerSurface())
                .accessibilityIdentifier("attach-caption")
            HStack(spacing: Theme.Space.flush) {
                ComposerRoundButton(symbol: Theme.Icon.send, label: String(localized: "Send"), id: "attach-send", pressScales: true) {
                    leave(.send(picked))
                }
            }
            .transition(.composerSend)
        }
        .padding(.horizontal, captionFocused ? Theme.Space.l : Theme.Space.xxl)
    }

    // MARK: Motion

    private func appear(rest: CGFloat) {
        offset = rest
        withAnimation(Theme.Motion.attachDimIn) { dimmed = true }
        withAnimation(reduceMotion ? nil : Theme.Motion.attachOpen()) { grown = 1 }
        withAnimation(reduceMotion ? nil : Theme.Motion.attachCorner) { cornered = 1 }
        withAnimation(reduceMotion ? nil : Theme.Motion.attachContentIn) { contentShown = true }
    }

    /// Shrinks back into the 「+」 button (or drops away), then hands the outcome to the composer.
    private func leave(_ outcome: AttachOutcome, velocity: CGFloat = 0) {
        guard !going else { return }
        going = true
        captionFocused = false
        withAnimation(Theme.Motion.attachDimOut) { dimmed = false }
        withAnimation(Theme.Motion.attachContentOut) { contentShown = false }
        withAnimation(reduceMotion ? nil : Theme.Motion.attachSettle()) { cornered = 0 }
        withAnimation(reduceMotion ? nil : Theme.Motion.attachOpen(velocity: velocity)) {
            grown = 0
            drop = 0
        } completion: {
            finish(outcome)
        }
    }

    private func settle(to target: CGFloat, velocity: CGFloat, motion: Animation) {
        withAnimation(reduceMotion ? nil : motion) {
            offset = target
            drop = 0
        }
    }

    private func pick(_ next: AttachTab) {
        if next == tab {
            settle(to: 0, velocity: 0, motion: Theme.Motion.attachSettle()) // the tab already shown: open all the way
            return
        }
        withAnimation(reduceMotion ? nil : Theme.Motion.attachTabFade) { tab = next }
        pageAtTop = true
        guard !reduceMotion else { return }
        withAnimation(Theme.Motion.attachTabDip) { dip = Theme.Attach.tabDip } completion: {
            withAnimation(Theme.Motion.attachTabBack) { dip = 0 }
        }
    }

    // MARK: Dragging

    private func beginsDrag(onHeader: Bool, velocity: CGPoint, open: Bool) -> Bool {
        guard abs(velocity.y) > abs(velocity.x) else { return false }
        if !open { return true }
        return onHeader || (pageAtTop && velocity.y > 0)
    }

    private func follow(_ travel: CGFloat, rest: CGFloat) {
        if dragStart == nil { dragStart = offset ?? rest }
        let place = AttachSheetPhysics.follow(startOffset: dragStart ?? rest, travel: travel, rest: rest)
        offset = place.offset
        drop = place.drop
    }

    private func release(_ travel: CGFloat, velocity: CGFloat, rest: CGFloat, height: CGFloat) {
        let start = dragStart ?? rest
        dragStart = nil
        let place = AttachSheetPhysics.follow(startOffset: start, travel: travel, rest: rest)
        switch AttachSheetPhysics.settle(offset: place.offset, drop: place.drop, velocity: velocity, rest: rest,
                                         startedOpen: start == 0, gridAtTop: pageAtTop) {
        case .dismiss:
            leave(.closed(picked), velocity: AttachSheetPhysics.springVelocity(velocity, remaining: height - place.offset))
        case .open:
            settle(to: 0, velocity: 0, motion: Theme.Motion.attachSettle(velocity: AttachSheetPhysics.springVelocity(velocity, remaining: place.offset)))
        case .rest where abs(velocity) > Theme.Attach.settleSpeed && place.drop == 0:
            settle(to: rest, velocity: 0, motion: Theme.Motion.attachSettle(velocity: AttachSheetPhysics.springVelocity(velocity, remaining: rest - place.offset)))
        case .rest:
            settle(to: rest, velocity: 0, motion: Theme.Motion.attachSnapBack)
        }
    }

    private func openSettings() {
        if let url = URL(string: UIApplication.openSettingsURLString) { UIApplication.shared.open(url) }
    }
}

/// The tab bar gone (photos picked): smaller, blurred.
private struct AttachBarAway: ViewModifier {
    var away = true
    func body(content: Content) -> some View {
        content
            .scaleEffect(away ? Theme.Attach.barAwayScale : 1)
            .blur(radius: away ? Theme.Attach.barAwayBlur : 0)
    }
}

/// The floating tab bar: a pill of tabs, a highlight sliding under the one shown.
struct AttachTabBar: View {
    var tabs: [AttachTab]
    var selected: AttachTab
    var width: CGFloat
    var pick: (AttachTab) -> Void
    @Namespace private var lens

    var body: some View {
        HStack(spacing: Theme.Space.flush) {
            ForEach(tabs) { tab in
                Button { pick(tab) } label: {
                    VStack(spacing: Theme.Space.xxs) {
                        ThemedIcon(tab.symbol)
                            .font(Theme.Font.heading)
                            .frame(width: Theme.Attach.barIcon, height: Theme.Attach.barIcon)
                        Text(tab.title).font(Theme.Font.labelMedium).lineLimit(1)
                    }
                    .foregroundStyle(tab == selected ? Theme.accent : Color.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background {
                        if tab == selected {
                            Capsule().fill(Theme.Fill.chip).matchedGeometryEffect(id: "lens", in: lens)
                        }
                    }
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(tab.title)
                .accessibilityAddTraits(tab == selected ? .isSelected : [])
                .accessibilityIdentifier("attach-tab-\(tab.rawValue)")
            }
        }
        .padding(Theme.Attach.barLensInset)
        .frame(width: width, height: Theme.Attach.barHeight)
        .modifier(ComposerSurface())
        .animation(Theme.Motion.attachLens, value: selected)
    }
}

/// The grid of recent photos: the camera first, a column wide and two rows tall, then three across.
private struct AttachGrid: View {
    let library: AttachLibrary
    @Binding var picked: [String]
    var width: CGFloat
    var camera: (() -> Void)?
    var limited: Bool
    var photoRoom: Int
    var videoRoom: Int

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("AttachGrid")
        #endif
        let cell = AttachSheetPhysics.cell(width: width)
        let spacing = Theme.Attach.gridSpacing
        let assets = library.assets
        let lead = camera == nil ? 0 : min(4, assets.count)
        let rest = Array(assets.dropFirst(lead))
        let rows = stride(from: 0, to: rest.count, by: Theme.Attach.columns).map { Array(rest[$0..<min($0 + Theme.Attach.columns, rest.count)]) }
        ScrollView {
            LazyVStack(alignment: .leading, spacing: spacing) {
                if limited { AttachLimitedBanner(refetch: library.fetch) }
                if let camera {
                    HStack(alignment: .top, spacing: spacing) {
                        AttachCameraCell(action: camera).frame(width: cell, height: cell * 2 + spacing)
                        VStack(alignment: .leading, spacing: spacing) {
                            HStack(spacing: spacing) { ForEach(assets.prefix(2), id: \.localIdentifier) { photo($0, cell: cell) } }
                            HStack(spacing: spacing) { ForEach(assets.dropFirst(2).prefix(2), id: \.localIdentifier) { photo($0, cell: cell) } }
                        }
                    }
                }
                ForEach(rows.indices, id: \.self) { i in
                    HStack(spacing: spacing) { ForEach(rows[i], id: \.localIdentifier) { photo($0, cell: cell) } }
                }
            }
        }
        .contentMargins(.bottom, Theme.Attach.gridBottom, for: .scrollContent)
        .scrollIndicators(.hidden)
        .accessibilityIdentifier("attach-grid")
    }

    private func photo(_ asset: PHAsset, cell: CGFloat) -> some View {
        let id = asset.localIdentifier
        return AttachPhotoCell(library: library, asset: asset, side: cell, number: picked.firstIndex(of: id).map { $0 + 1 }) {
            toggle(asset)
        }
    }

    private func toggle(_ asset: PHAsset) {
        let id = asset.localIdentifier
        if let i = picked.firstIndex(of: id) { picked.remove(at: i); return }
        let video = asset.mediaType == .video
        let same = picked.compactMap(AttachLibrary.asset).filter { ($0.mediaType == .video) == video }.count
        guard same < (video ? videoRoom : photoRoom) else {
            AppModel.shared.toast = Toast(String(localized: "Up to 4 at a time"), style: .error)
            return
        }
        picked.append(id)
        Haptics.tap()
    }
}

/// One photo or video in the grid: its thumbnail, a video's length, and the mark that says it is picked and in what order.
private struct AttachPhotoCell: View {
    let library: AttachLibrary
    let asset: PHAsset
    var side: CGFloat
    var number: Int?
    var toggle: () -> Void
    @State private var image: UIImage?

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("AttachPhotoCell")
        #endif
        Button(action: toggle) {
            ZStack(alignment: .topTrailing) {
                Group {
                    if let image { Image(uiImage: image).resizable().scaledToFill() } else { Theme.Fill.well }
                }
                .frame(width: side, height: side)
                .clipped()
                if asset.mediaType == .video {
                    Text(Duration.seconds(asset.duration).formatted(.time(pattern: .minuteSecond)))
                        .font(Theme.Font.labelStrong)
                        .foregroundStyle(.white)
                        .shadow(radius: Theme.Space.xxs)
                        .padding(Theme.Space.xs)
                        .frame(width: side, height: side, alignment: .bottomLeading)
                }
                AttachPickMark(number: number)
                    .padding(Theme.Attach.badgeInset)
            }
        }
        .buttonStyle(.plain)
        .frame(width: side, height: side)
        .task(id: asset.localIdentifier) { image = await library.thumbnail(asset, side: side) }
        .accessibilityLabel(asset.mediaType == .video ? String(localized: "Video") : String(localized: "Photo"))
        .accessibilityValue(number.map { String(localized: "Picked, \($0)") } ?? "")
        .accessibilityAddTraits(number != nil ? .isSelected : [])
        .accessibilityIdentifier("attach-photo")
    }
}

/// The round mark in a cell's corner: an empty ring, or filled with the photo's place in the order, bouncing as it fills.
private struct AttachPickMark: View {
    var number: Int?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        ZStack {
            Circle().fill(number == nil ? Color.black.opacity(Theme.Attach.dim) : Theme.accent)
            Circle().strokeBorder(.white, lineWidth: Theme.Attach.badgeRing)
            if let number {
                Text(verbatim: "\(number)").font(Theme.Font.labelStrong).foregroundStyle(.white).monospacedDigit()
            }
        }
        .frame(width: Theme.Attach.badge, height: Theme.Attach.badge)
        .keyframeAnimator(initialValue: CGFloat(1), trigger: number) { mark, scale in
            mark.scaleEffect(scale)
        } keyframes: { _ in
            if number != nil, !reduceMotion {
                CubicKeyframe(Theme.Motion.attachPickSqueeze, duration: Theme.Motion.attachPickSqueezeTime)
                CubicKeyframe(Theme.Motion.attachPickBounce, duration: Theme.Motion.attachPickBounceTime)
                CubicKeyframe(1, duration: Theme.Motion.attachPickSettleTime)
            } else {
                CubicKeyframe(1, duration: Theme.Motion.attachPickSettleTime)
            }
        }
        .animation(number == nil ? Theme.Motion.attachUnpick : Theme.Motion.attachPick, value: number)
        .shadow(color: .black.opacity(Theme.Attach.dim), radius: Theme.Space.xxs)
    }
}

/// The camera's cell: takes a photo with the system camera (the panel goes first). Telegram shows the live camera here.
private struct AttachCameraCell: View {
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            ZStack {
                Theme.Fill.chip
                ThemedIcon(Theme.Icon.camera).font(Theme.Font.title).foregroundStyle(.secondary)
            }
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Take Photo")
        .accessibilityIdentifier("attach-camera")
    }
}

/// Limited access: a line over the grid saying so, and Manage to pick more.
private struct AttachLimitedBanner: View {
    var refetch: () -> Void

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            Text("Persona can see only the photos you chose.")
                .font(Theme.Font.secondary)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button("Manage") {
                guard let top = AttachTop.controller() else { return }
                PHPhotoLibrary.shared().presentLimitedLibraryPicker(from: top) { _ in Task { @MainActor in refetch() } }
            }
            .font(Theme.Font.secondaryStrong)
            .lineLimit(1)
            .accessibilityIdentifier("attach-manage")
        }
        .padding(.horizontal, Theme.Space.l)
        .padding(.vertical, Theme.Space.m)
    }
}

/// No access to the photos: say so, offer Settings, and the system's picker (which needs none).
private struct AttachNoAccess: View {
    var settings: () -> Void
    var allPhotos: () -> Void

    var body: some View {
        VStack(spacing: Theme.Space.m) {
            ThemedIcon(Theme.Icon.photos).font(Theme.Font.display).foregroundStyle(.secondary)
            Text("Access Your Photos").font(Theme.Font.heading)
            Text("Let Persona see your photos to pick them here, or choose them in the system's picker.")
                .font(Theme.Font.secondary)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            Button("Open Settings", action: settings)
                .themedButton(prominent: true)
                .lineLimit(1)
            Button("Choose Photos", action: allPhotos)
                .font(Theme.Font.bodyStrong)
                .lineLimit(1)
                .accessibilityIdentifier("attach-choose-photos")
        }
        .padding(.horizontal, Theme.Space.xxl)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// The sheet's pan, a UIKit recognizer so it can take over from a grid scrolled to its top (Telegram's hand-off): it
/// begins only when `shouldBegin` says so (the sheet not open all the way, the header, or a pull down from the top),
/// and then stops the scroll view under it so the two never move together.
private struct AttachPan: UIGestureRecognizerRepresentable {
    var enabled: Bool
    var shouldBegin: (CGPoint, CGPoint) -> Bool
    var changed: (CGFloat) -> Void
    var ended: (CGFloat, CGFloat) -> Void

    func makeCoordinator(converter: CoordinateSpaceConverter) -> Coordinator { Coordinator() }

    func makeUIGestureRecognizer(context: Context) -> UIPanGestureRecognizer {
        let pan = UIPanGestureRecognizer()
        pan.delegate = context.coordinator
        pan.maximumNumberOfTouches = 1
        return pan
    }

    func updateUIGestureRecognizer(_ recognizer: UIPanGestureRecognizer, context: Context) {
        recognizer.isEnabled = enabled
        context.coordinator.shouldBegin = shouldBegin
    }

    func handleUIGestureRecognizerAction(_ recognizer: UIPanGestureRecognizer, context: Context) {
        let travel = recognizer.translation(in: recognizer.view).y
        switch recognizer.state {
        case .began:
            context.coordinator.stopScrolling()
            changed(travel)
        case .changed:
            changed(travel)
        case .ended, .cancelled, .failed:
            ended(travel, recognizer.velocity(in: recognizer.view).y)
        default:
            break
        }
    }

    final class Coordinator: NSObject, UIGestureRecognizerDelegate {
        var shouldBegin: (CGPoint, CGPoint) -> Bool = { _, _ in false }
        private weak var scroll: UIScrollView?

        func gestureRecognizerShouldBegin(_ g: UIGestureRecognizer) -> Bool {
            guard let pan = g as? UIPanGestureRecognizer else { return false }
            return shouldBegin(pan.location(in: nil), pan.velocity(in: pan.view)) // the window's: the sheet's top is in it
        }

        func gestureRecognizer(_ g: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool {
            if let view = other.view as? UIScrollView, other == view.panGestureRecognizer { scroll = view }
            return true
        }

        /// The sheet took the finger: the scroll view under it lets go, so the grid stays at its top.
        func stopScrolling() {
            guard let scroll, scroll.isScrollEnabled else { return }
            scroll.panGestureRecognizer.isEnabled = false
            scroll.panGestureRecognizer.isEnabled = true
        }
    }
}

/// Puts the panel over the whole window from the composer: a full-screen cover with no background and no animation of
/// its own (the panel draws its own), and what the panel hands back.
struct AttachSheetHost<More: View>: ViewModifier {
    let panel: AttachPanel
    @Binding var caption: String
    var photoRoom: Int
    var videoRoom: Int
    var camera: (() -> Void)?
    var files: () -> Void
    var allPhotos: () -> Void
    var showsMore: Bool
    @ViewBuilder var more: () -> More
    var finish: (AttachOutcome) -> Void

    func body(content: Content) -> some View {
        content.fullScreenCover(isPresented: Binding(get: { panel.shown }, set: { if !$0 { panel.shown = false } })) {
            AttachSheet(panel: panel, caption: $caption, photoRoom: photoRoom, videoRoom: videoRoom, camera: camera, files: files,
                        allPhotos: allPhotos, showsMore: showsMore, more: more) { outcome in
                var quiet = Transaction()
                quiet.disablesAnimations = true
                withTransaction(quiet) { panel.shown = false }
                panel.leaving = nil
                // What runs after it (the camera, Files) presents once the cover is down.
                Task { @MainActor in
                    try? await Task.sleep(for: Theme.Attach.afterKeyboard)
                    finish(outcome)
                }
            }
            .presentationBackground(.clear)
        }
    }
}

/// The view controller on top, for UIKit sheets the panel presents itself (the limited-library picker).
private enum AttachTop {
    static func controller() -> UIViewController? {
        let window = UIApplication.shared.connectedScenes.compactMap { ($0 as? UIWindowScene)?.keyWindow }.first
        var top = window?.rootViewController
        while let next = top?.presentedViewController, !next.isBeingDismissed { top = next }
        return top
    }
}

/// A row of the panel's More tab (the menu's other items); it closes the panel, then acts.
struct AttachMoreRow: Identifiable {
    let id: String
    let title: String
    let symbol: String
    var destructive = false
    var disabled = false
    let action: () -> Void
}

struct AttachMoreSection: Identifiable {
    let id: String
    let title: String?
    let rows: [AttachMoreRow]
}

struct AttachMoreRows: View {
    let panel: AttachPanel
    let sections: [AttachMoreSection]

    var body: some View {
        ForEach(sections) { section in
            Section {
                ForEach(section.rows) { row in
                    Button(role: row.destructive ? .destructive : nil) { panel.close(then: row.action) } label: {
                        Label { Text(row.title).lineLimit(1) } icon: { ThemedIcon(row.symbol) }
                    }
                    .disabled(row.disabled)
                    .accessibilityIdentifier(row.id)
                }
            } header: {
                if let title = section.title { Text(title) }
            }
        }
    }
}

extension AttachPanel {
    /// Opens the panel with no animation of the system's: it grows out of `source` on its own.
    func open(from source: CGRect) {
        self.source = source
        leaving = nil
        var quiet = Transaction()
        quiet.disablesAnimations = true
        withTransaction(quiet) { shown = true }
    }
}
#endif
