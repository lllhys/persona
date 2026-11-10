// Frozen sharing HTTP DTOs. These types deliberately have no authenticated application dependencies.
export type Selection = { mode: "all" } | { mode: "from"; from_message_id: string } | { mode: "individual"; message_ids: string[] };
export type Execution = { kind: "text"; text: string } | { kind: "tool"; tool_id: string; name: string; arguments: string; results: { text: string }[] } | { kind: "agent"; tool_id: string; text: string };
export interface PublicMessage { message_id: string; role: "user" | "assistant"; text: string; execution?: Execution[] }
export interface ShareMetadata {
  share_id: string; revision: number; content_version: number;
  selection: { mode: Selection["mode"]; count: number };
  include_future: boolean; show_tools: boolean;
  created_at: string; updated_at: string; expires_at: string;
  status: "active" | "revoked" | "expired";
  publication_status: "frozen" | "live" | "paused"; pause_reason: string | null;
}
export interface Catalog {
  can_share: boolean; reason: string | null; can_include_future: boolean;
  snapshot_id?: string; snapshot_version?: string; expires_at?: string; complete?: boolean;
  total_messages?: number; omitted_messages?: number; omissions?: string[];
  messages: (PublicMessage & { selectable: boolean })[]; next_cursor?: string | null;
}
export interface CreatePolicy {
  snapshot_id: string; snapshot_version: string; selection: Selection;
  include_future: boolean; show_tools: boolean; expires_in_days: 1 | 3 | 7 | 30;
}
export interface Acknowledgement { sensitive_content: true; policy_version: number; preview_id: string; insecure_transport?: true }
export interface UpdatePolicy {
  expected_revision: number; expected_content_version?: number;
  snapshot_id?: string; snapshot_version?: string; selection?: Selection;
  remove_message_ids?: string[]; include_future?: boolean; show_tools?: boolean; expires_at?: string;
}
export interface Preview {
  preview_id: string; expires_at: string; policy_version: number;
  selected_count: number; omitted_count: number; messages: PublicMessage[]; next_cursor: string | null;
  requires_insecure_transport_ack?: boolean;
  /** The hub's localized HTTP transport warning; null or absent for HTTPS origins. */
  transport_warning?: string | null;
}
export interface MessagePage { revision: number; content_version: number; messages: PublicMessage[]; next_cursor: string | null }
export interface PublicPage extends MessagePage {
  expires_at: string; publication_status: ShareMetadata["publication_status"]; poll_after_seconds: number;
}
