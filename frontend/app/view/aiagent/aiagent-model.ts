// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { BlockNodeModel } from "@/app/block/blocktypes";
import { globalStore } from "@/app/store/jotaiStore";
import type { TabModel } from "@/app/store/tab-model";
import { TabRpcClient } from "@/app/store/wshrpcutil";
import { AiAgentView } from "@/app/view/aiagent/aiagent";
import type { AiAgentEnv } from "@/app/view/aiagent/aiagentenv";
import { fireAndForget, isBlank } from "@/util/util";
import { atom, type Atom, type PrimitiveAtom } from "jotai";
import { v7 as uuidv7 } from "uuid";

export type ChatEntry = {
    id: string;
    // "you" is the local echo of a prompt; the rest come from the agent's protocol stream.
    role: "you" | "agent" | "tools" | "status" | "error";
    text: string;
};

export type SessionStatus = "idle" | "starting" | "running" | "stopped" | "error";

export class AiAgentViewModel implements ViewModel {
    blockId: string;
    viewType = "aiagent";
    viewIcon = atom("robot");
    viewName = atom("Agent");
    viewComponent = AiAgentView;
    noPadding = atom(true);
    nodeModel: BlockNodeModel;
    tabModel: TabModel;
    env: AiAgentEnv;

    agentsAtom: PrimitiveAtom<DetectedAgent[]>;
    selectedAgentAtom: PrimitiveAtom<string>;
    entriesAtom: PrimitiveAtom<ChatEntry[]>;
    statusAtom: PrimitiveAtom<SessionStatus>;
    inputAtom: PrimitiveAtom<string>;
    errorAtom: PrimitiveAtom<string>;
    costAtom: PrimitiveAtom<number>;
    // Where the agent will run, derived from the tab's project.
    targetAtom: Atom<{ cwd: string; connection: string }>;

    private sessionId: string = null;

    constructor({ blockId, nodeModel, tabModel, waveEnv }: ViewModelInitType) {
        this.blockId = blockId;
        this.nodeModel = nodeModel;
        this.tabModel = tabModel;
        this.env = waveEnv as AiAgentEnv;

        this.agentsAtom = atom<DetectedAgent[]>([]);
        this.selectedAgentAtom = atom("");
        this.entriesAtom = atom<ChatEntry[]>([]);
        this.statusAtom = atom<SessionStatus>("idle");
        this.inputAtom = atom("");
        this.errorAtom = atom(null) as PrimitiveAtom<string>;
        this.costAtom = atom(0);

        // The agent runs where the project lives. Without this a WSL project would get an
        // agent on the Windows side, looking at files that are not there.
        this.targetAtom = atom((get) => {
            const tabId = get(this.env.atoms.staticTabId);
            if (isBlank(tabId)) {
                return { cwd: "", connection: "" };
            }
            const tabAtom = this.env.wos.getWaveObjectAtom<Tab>(`tab:${tabId}`);
            const meta = get(tabAtom)?.meta;
            return {
                cwd: meta?.["project:path"] ?? "",
                connection: meta?.["project:connection"] ?? "",
            };
        });
    }

    private append(entry: Omit<ChatEntry, "id">) {
        globalStore.set(this.entriesAtom, (prev) => [...prev, { ...entry, id: uuidv7() }]);
    }

    // Refresh the agent list for the current target. Called on mount and by the retry button.
    async loadAgents(): Promise<void> {
        const { connection } = globalStore.get(this.targetAtom);
        globalStore.set(this.errorAtom, null);
        try {
            const list = await this.env.rpc.AiAgentListCommand(TabRpcClient, { connection });
            globalStore.set(this.agentsAtom, list ?? []);
            const current = globalStore.get(this.selectedAgentAtom);
            if (isBlank(current)) {
                const first = (list ?? []).find((a) => a.found && a.supported);
                if (first != null) {
                    globalStore.set(this.selectedAgentAtom, first.id);
                }
            }
        } catch (e) {
            globalStore.set(this.errorAtom, `Could not look for agents: ${e?.message ?? e}`);
        }
    }

    async start(): Promise<void> {
        if (globalStore.get(this.statusAtom) == "running" || globalStore.get(this.statusAtom) == "starting") {
            return;
        }
        const agentId = globalStore.get(this.selectedAgentAtom);
        if (isBlank(agentId)) {
            globalStore.set(this.errorAtom, "Pick an agent first");
            return;
        }
        const { cwd, connection } = globalStore.get(this.targetAtom);
        this.sessionId = uuidv7();
        globalStore.set(this.statusAtom, "starting");
        globalStore.set(this.errorAtom, null);
        this.append({ role: "status", text: `starting ${agentId}${connection ? ` on ${connection}` : ""}` });

        const stream = this.env.rpc.AiAgentRunCommand(
            TabRpcClient,
            { sessionid: this.sessionId, agentid: agentId, connection, cwd, interactive: true },
            null
        );
        fireAndForget(async () => {
            try {
                for await (const ev of stream) {
                    this.handleEvent(ev);
                }
                // The stream closing means the process exited.
                globalStore.set(this.statusAtom, (prev) => (prev == "error" ? prev : "stopped"));
                this.append({ role: "status", text: "session ended" });
            } catch (e) {
                globalStore.set(this.statusAtom, "error");
                globalStore.set(this.errorAtom, `${e?.message ?? e}`);
            } finally {
                this.sessionId = null;
            }
        });
    }

    private handleEvent(ev: AgentEvent) {
        switch (ev.kind) {
            case "init":
                globalStore.set(this.statusAtom, "running");
                this.append({
                    role: "status",
                    text: `ready${ev.model ? ` · ${ev.model}` : ""}${ev.cwd ? ` · ${ev.cwd}` : ""}`,
                });
                break;
            case "assistant":
                if (!isBlank(ev.text)) {
                    this.append({ role: "agent", text: ev.text });
                }
                if (ev.toolnames?.length > 0) {
                    this.append({ role: "tools", text: ev.toolnames.join(", ") });
                }
                break;
            case "result":
                if (ev.costusd) {
                    globalStore.set(this.costAtom, ev.costusd);
                }
                if (ev.iserror) {
                    globalStore.set(this.statusAtom, "error");
                    this.append({ role: "error", text: ev.text || `failed (${ev.subtype ?? "unknown"})` });
                }
                break;
            case "ratelimit":
                // Only surfaced when it is not the normal "allowed": a session dying on quota
                // otherwise looks like a hang.
                if (ev.ratelimitstatus && ev.ratelimitstatus != "allowed") {
                    this.append({ role: "error", text: `rate limit: ${ev.ratelimitstatus}` });
                }
                break;
            default:
                break;
        }
    }

    updateSelected(agentId: string) {
        globalStore.set(this.selectedAgentAtom, agentId);
    }

    updateInput(text: string) {
        globalStore.set(this.inputAtom, text);
    }

    async send(): Promise<void> {
        const text = globalStore.get(this.inputAtom).trim();
        if (text == "" || this.sessionId == null) {
            return;
        }
        globalStore.set(this.inputAtom, "");
        this.append({ role: "you", text });
        try {
            await this.env.rpc.AiAgentSendCommand(TabRpcClient, { sessionid: this.sessionId, text });
        } catch (e) {
            globalStore.set(this.errorAtom, `Could not send: ${e?.message ?? e}`);
        }
    }

    async stop(): Promise<void> {
        if (this.sessionId == null) {
            return;
        }
        const id = this.sessionId;
        try {
            await this.env.rpc.AiAgentStopCommand(TabRpcClient, id);
        } catch (e) {
            globalStore.set(this.errorAtom, `Could not stop: ${e?.message ?? e}`);
        }
    }

    dispose() {
        // A block closing must not leave an agent running and billing.
        if (this.sessionId != null) {
            fireAndForget(() => this.env.rpc.AiAgentStopCommand(TabRpcClient, this.sessionId));
        }
    }
}
