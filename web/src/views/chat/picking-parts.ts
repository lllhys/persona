// What a chat draws while it picks messages to forward at once (#3838): the checkbox over each message and the bar in the
// composer's place (views/chat/Picking.tsx, a chunk that comes with Forward to). Forward to's Select messages… puts them
// here before the chat starts picking, so the chat draws them at once; until then the chat draws nothing of picking.
import type { ComponentType } from "react";
import type { PickBarProps, PickRowProps } from "./Picking";

export const pickParts: { Row: ComponentType<PickRowProps> | null; Bar: ComponentType<PickBarProps> | null } = { Row: null, Bar: null };
