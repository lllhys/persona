package node

// The file.* methods (Node.handle, #8089): each name is a constant, and each method that reads request fields decodes
// them into its own struct. Wire JSON is unchanged; the tags are what the shared decoding in handle used.
const (
	methodFileUploadBegin = "file.upload.begin"
	methodFileUploadChunk = "file.upload.chunk"
	methodFileUploadEnd   = "file.upload.end"
	methodFileUploadAbort = "file.upload.abort"
	methodFileRename      = "file.rename"
	methodFileMove        = "file.move"
	methodFileTrash       = "file.trash"
	methodFileRestore     = "file.restore"
	methodFileMkdir       = "file.mkdir"
	methodFileWrite       = "file.write"
	methodFileZip         = "file.zip"
	methodFileCopy        = "file.copy"
	methodFileChmod       = "file.chmod"
	methodFileOperation   = "file.operation"
	methodFileChunk       = "file.chunk"
	methodFileConvert     = "file.convert"
	methodFileThumb       = "file.thumb"
	methodFileRead        = "file.read"
)

// fileReadParams is file.read's request (files.go readFile).
type fileReadParams struct {
	Path   string `json:"path"`
	Cwd    string `json:"cwd"`
	Offset int64  `json:"offset"`
	Stream bool   `json:"stream"`
}
