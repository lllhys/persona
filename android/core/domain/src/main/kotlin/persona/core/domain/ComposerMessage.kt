package persona.core.domain

/**
 * A message with attachments as the chat composer writes it (feature/chat `ChatComposerState.message`), for messages
 * written outside the composer: New Agent's first message (#2321). The words stay as typed; a `<persona-file>` tag per
 * video and then per file follows after a blank line, then the photos' paths in an `<attached_images>` block, the only
 * paths the apps draw as thumbnails above the message (docs/protocol/protocol.md, `<attached_images>`).
 */
object ComposerMessage {
    /**
     * [words], then [videos] and [files] (already on the computer), with [photos] (paths on the computer) last. With no
     * words, photos take [photosOnly], else files take [filesOnly], else videos take [videosOnly], in the app's language.
     */
    fun text(
        words: String, photos: List<String>, files: List<AttachedFile>, videos: List<AttachedFile> = emptyList(),
        photosOnly: String = "", videosOnly: String = "", filesOnly: String = "",
    ): String {
        val said = when {
            words.isNotBlank() -> words
            photos.isNotEmpty() -> photosOnly
            files.isNotEmpty() -> filesOnly
            videos.isNotEmpty() -> videosOnly
            else -> words
        }
        val attached = (videos + files).joinToString("\n") { it.toXml() }
        val images = if (photos.isEmpty()) "" else "<attached_images>\n${photos.joinToString("\n")}\n</attached_images>"
        return listOf(said, attached, images).filter(String::isNotBlank).joinToString("\n\n")
    }

    /** A path the shell would split or expand is single-quoted, for the destinations that still send paths as words. */
    fun quotePath(path: String): String =
        if (path.any { it.isWhitespace() || it in "'\"\\\$`;&|<>()[]{}*?!" }) "'${path.replace("'", "'\\''")}'" else path
}
