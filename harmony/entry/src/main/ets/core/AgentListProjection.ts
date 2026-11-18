export interface AgentListRow {
  id: string;
  raw: Record<string, Object>;
  source: Object;
}
export interface AgentListPreferences {
  pinsSpaces: string[];
  pinsSessions: string[];
  mutesSpaces: string[];
  mutesSessions: string[];
  mutesAllowed: string[];
}
export interface AgentListInput {
  rows: AgentListRow[];
  history: boolean;
  grouping: string;
  quick: string;
  machineFilter: string;
  query: string;
  reading: boolean;
  kept: string[];
  machines: Record<string, Object>[];
  coreSettings: Record<string, Object>;
  preferencesFor(row: AgentListRow): AgentListPreferences;
  machineIdentity(row: AgentListRow): string;
  machineName(row: AgentListRow): string;
}
export interface CoreAgentListGroup {
  id: string;
  title: string;
  rows: string[];
  visible_rows: string[];
  folded: boolean;
  parent?: string;
  state_counts?: Record<string, Object>;
  spoken?: string;
}
export interface CoreAgentListProjection {
  counts: Record<string, Object>;
  groups: CoreAgentListGroup[];
  unread_shown: string[];
  quiet_title?: string | null;
}
export interface CoreSortInput {
  sessions: Record<string, Object>[];
  machines: Record<string, Object>[];
  pins: Record<string, Object>;
  mutes: Record<string, Object>;
  settings: Record<string, Object>;
  /** Per-Hub preferences from #9027; the legacy maps belong to the default Hub. */
  hub_preferences?: Record<string, Object>;
  /** Manager-qualified preferences override the Hub default only for that manager. */
  manager_preferences?: Record<string, Object>;
}
export type CoreProjectSort = (input: string) => string;
