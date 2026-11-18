import Foundation
import PersonaCoreC

/// Pure projections need no core handle. They run synchronously on their caller, so a client must keep sizable
/// projections off the main actor; none of these functions performs file IO or creates a worker.
public enum PersonaCoreProjections {
    public static func sort(inputJSON: String) throws -> String {
        try project(inputJSON, using: persona_core_project_sort)
    }

    public static func answer(inputJSON: String) throws -> String {
        try project(inputJSON, using: persona_core_project_answer)
    }

    /// Unlike the other inputs, this is a bare JSON string containing the option label.
    public static func displayAnswer(optionLabelJSON: String) throws -> String {
        try project(optionLabelJSON, using: persona_core_display_answer)
    }

    public static func streamChunks(inputJSON: String) throws -> String {
        try project(inputJSON, using: persona_core_project_stream_chunks)
    }

    public static func turns(inputJSON: String) throws -> String {
        try project(inputJSON, using: persona_core_project_turns)
    }

    public static func taskIndex(inputJSON: String) throws -> String {
        try project(inputJSON, using: persona_core_project_task_index)
    }

    public static func viewedImages(inputJSON: String) throws -> String {
        try project(inputJSON, using: persona_core_project_viewed_images)
    }

    private static func project(
        _ input: String,
        using projection: (UnsafePointer<UInt8>?, Int) -> PersonaCoreBytes
    ) throws -> String {
        let input = Array(input.utf8)
        return try input.withUnsafeBufferPointer { buffer in
            let output = projection(buffer.baseAddress, buffer.count)
            // The C contract transfers every returned allocation to its caller, including an empty failure result.
            defer { persona_core_bytes_free(output) }
            guard let pointer = output.data else { throw PersonaCoreProjectionError.emptyResult }
            let data = Data(bytes: pointer, count: output.length)
            guard let result = String(data: data, encoding: .utf8) else {
                throw PersonaCoreProjectionError.invalidUTF8Result
            }
            return result
        }
    }
}

public enum PersonaCoreProjectionError: Error, Equatable {
    case emptyResult
    case invalidUTF8Result
}
