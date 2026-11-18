import { Colors } from './Colors';

/** Known local action symbols; server-defined actions keep their names in native menus. */
export class IconActions {
  static busy(key: string): boolean { return ['loading', 'sending', 'pending', 'admin_node_deploying'].includes(key); }
  static color(key: string, primary: boolean): ResourceColor {
    if (primary) return Colors.onAction;
    return ['delete', 'forget_hub', 'knowledge_recording_delete_local', 'deny', 'reject', 'voice_end'].includes(key) ? Colors.danger : Colors.link;
  }
  static symbol(key: string): Resource {
    if (['inbox_show_translation', 'inbox_show_original'].includes(key)) return $r('sys.symbol.arrow_left_arrow_right');
    if (key === 'inbox_send_attachment') return $r('sys.symbol.doc_text_badge_arrow_up');
    if (key === 'reply') return $r('sys.symbol.arrowshape_turn_up_left');
    if (key === 'handoff') return $r('sys.symbol.arrowshape_turn_up_right');
    if (["health_archive"].includes(key)) return $r('sys.symbol.archivebox');
    if (["retry", "refresh", "retry_cleanup", "extension_ui_retry_action", "assistant_view_retry_original", "knowledge_recording_retry_upload", "check_status", "admin_hub_check", "admin_launcher_check", "admin_health_check", "assistant_detail_refresh", "assistant_detail_retry", "computer_screen_refresh", "file_transfer_check_status"].includes(key)) return $r('sys.symbol.arrow_clockwise');
    if (["load_more", "more", "inbox_older", "work_summary", "latest"].includes(key)) return $r('sys.symbol.arrow_down');
    if (["download", "install", "upgrade", "admin_load_import", "admin_import_selected"].includes(key)) return $r('sys.symbol.arrow_down_to_line');
    if (["admin_move", "session_tools_switch_branch"].includes(key)) return $r('sys.symbol.arrow_right');
    if (["send", "assistant_feed_send", "computer_screen_send_text", "inbox_handoff_send", "knowledge_ask_persona", "file_transfer_confirm_send", "session_tools_forward"].includes(key)) return $r('sys.symbol.arrow_up');
    if (["open", "knowledge_open_link", "extension_ui_open_lines", "session_tools_preview_external_link"].includes(key)) return $r('sys.symbol.arrow_up_right');
    if (["health_upload_report"].includes(key)) return $r('sys.symbol.arrow_up_to_line');
    if (["back", "assistant_feed_undo_dismiss", "session_tools_restore", "reopen", "restore_draft"].includes(key)) return $r('sys.symbol.arrow_uturn_down');
    if (["translation"].includes(key)) return $r('sys.symbol.translate');
    if (["save", "admin_save", "process", "todo_complete", "checked", "file_transfer_i_checked", "confirm", "session_tools_commit", "session_tools_stash_save", "ssh_save_enable"].includes(key)) return $r('sys.symbol.checkmark');
    if (["verify", "allow", "confirm_computer", "approve"].includes(key)) return $r('sys.symbol.checkmark_shield');
    if (["interaction_question_later", "pending", "admin_node_deploying"].includes(key)) return $r('sys.symbol.clock');
    if (["knowledge_minutes_make", "knowledge_recording_transcribe", "knowledge_recording_retry_transcribe"].includes(key)) return $r('sys.symbol.doc_plaintext');
    if (["result_unknown", "unknown"].includes(key)) return $r('sys.symbol.exclamationmark_circle');
    if (["preview", "review", "image", "computer_screen_open", "knowledge_recording_open", "session_tools_preview_file", "session_tools_preview_open"].includes(key)) return $r('sys.symbol.eye');
    if (["open_settings", "test", "admin_make_default"].includes(key)) return $r('sys.symbol.gearshape');
    if (["control", "computer_screen_control", "takeover"].includes(key)) return $r('sys.symbol.hand_point_up_tap');
    if (["sign_in", "computer_screen_sign_in"].includes(key)) return $r('sys.symbol.key');
    if (["connect", "attach_live", "todo_link_agent", "todo_link_project", "attachment", "computer_screen_connect"].includes(key)) return $r('sys.symbol.link');
    if (["knowledge_minutes_add_todos"].includes(key)) return $r('sys.symbol.list_bullet');
    if (["search"].includes(key)) return $r('sys.symbol.magnifyingglass');
    if (["interaction_question_reopen"].includes(key)) return $r('sys.symbol.message');
    if (["knowledge_recording_start", "voice_start", "voice_unmute"].includes(key)) return $r('sys.symbol.mic_fill');
    if (["voice_mute"].includes(key)) return $r('sys.symbol.mic_slash');
    if (["session_tools_unstaged"].includes(key)) return $r('sys.symbol.minus');
    if (["deny", "reject"].includes(key)) return $r('sys.symbol.nosign');
    if (["voice_end"].includes(key)) return $r('sys.symbol.phone_down_fill');
    if (["assistant_feed_start", "knowledge_start_agent", "start", "run_now", "experience", "admin_redeploy", "assistant_detail_continue", "continue"].includes(key)) return $r('sys.symbol.play_fill');
    if (["add", "add_computer", "admin_create_manager", "admin_create_memory", "admin_new_launcher", "new_pairing_code", "generate_code", "admin_new_install_code", "create", "session_tools_create_branch", "session_tools_staged"].includes(key)) return $r('sys.symbol.plus');
    if (["sign_out", "unbind"].includes(key)) return $r('sys.symbol.rectangle_and_arrowshape_turn_up_right');
    if (["edit", "admin_edit", "admin_rename", "health_memory_edit"].includes(key)) return $r('sys.symbol.square_and_pencil');
    if (["knowledge_recording_stop", "voice_interrupt", "ssh_disable", "release", "computer_screen_release_control"].includes(key)) return $r('sys.symbol.square_fill');
    if (["extension_market"].includes(key)) return $r('sys.symbol.square_grid_2x2');
    if (["copy"].includes(key)) return $r('sys.symbol.doc_text_on_doc_text');
    if (["delete", "clear", "forget_hub", "knowledge_forget_recording", "knowledge_recording_delete_local"].includes(key)) return $r('sys.symbol.trash');
    if (["cancel", "close", "remove", "assistant_feed_dismiss", "file_transfer_cancel_transfer", "computer_screen_cancel_sign_in"].includes(key)) return $r('sys.symbol.xmark');
    return $r('sys.symbol.questionmark_circle');
  }
}
