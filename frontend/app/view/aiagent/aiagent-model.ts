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

// Mirrors aiagent.SessionPlaceholder on the go side.
const SESSION_PLACEHOLDER = "{session}";

// This panel launches the agent into a real terminal block instead of driving its stdio
// protocol. The protocol route was built first and hit a wall: the CLI asks for tool
// permission through an MCP tool named by --permission-prompt-tool, and Wave has no MCP
// server, so "ask me each time" could not work at all. Running the CLI in a terminal gets
// permission prompts, slash commands, plan mode and interruption for free, and Wave already
// ships badge hooks for it.
//
// What the panel adds on top of a bare terminal: the project's directory and connection, the
// agent picker, the permission mode, and past sessions from the CLI's own store.

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
    permissionModeAtom: PrimitiveAtom<string>;
    historyAtom: PrimitiveAtom<HistorySession[]>;
    errorAtom: PrimitiveAtom<string>;
    isLoadingAtom: PrimitiveAtom<boolean>;
    // Where the agent will run, derived from the tab's project.
    targetAtom: Atom<{ cwd: string; connection: string }>;

    constructor({ blockId, nodeModel, tabModel, waveEnv }: ViewModelInitType) {
        this.blockId = blockId;
        this.nodeModel = nodeModel;
        this.tabModel = tabModel;
        this.env = waveEnv as AiAgentEnv;

        this.agentsAtom = atom<DetectedAgent[]>([]);
        this.selectedAgentAtom = atom("");
        // Empty leaves the CLI default rather than guessing a mode for the user.
        this.permissionModeAtom = atom("");
        this.historyAtom = atom<HistorySession[]>([]);
        this.errorAtom = atom(null) as PrimitiveAtom<string>;
        this.isLoadingAtom = atom(false);

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

    updateSelected(agentId: string) {
        globalStore.set(this.selectedAgentAtom, agentId);
        // Each agent has its own store, so the list has to follow the selection.
        globalStore.set(this.historyAtom, []);
        fireAndForget(() => this.loadHistory());
    }

    updatePermissionMode(mode: string) {
        globalStore.set(this.permissionModeAtom, mode);
    }

    async refresh(): Promise<void> {
        globalStore.set(this.isLoadingAtom, true);
        try {
            // Detection first: the history reader is chosen by the selected agent.
            await this.loadAgents();
            await this.loadHistory();
        } finally {
            globalStore.set(this.isLoadingAtom, false);
        }
    }

    async loadAgents(): Promise<void> {
        const { connection } = globalStore.get(this.targetAtom);
        globalStore.set(this.errorAtom, null);
        try {
            const list = await this.env.rpc.AiAgentListCommand(TabRpcClient, { connection });
            globalStore.set(this.agentsAtom, list ?? []);
            if (isBlank(globalStore.get(this.selectedAgentAtom))) {
                const first = (list ?? []).find((a) => a.found && a.supported);
                if (first != null) {
                    globalStore.set(this.selectedAgentAtom, first.id);
                }
            }
        } catch (e) {
            globalStore.set(this.errorAtom, `Could not look for agents: ${e?.message ?? e}`);
        }
    }

    async loadHistory(): Promise<void> {
        const { cwd, connection } = globalStore.get(this.targetAtom);
        const agentId = globalStore.get(this.selectedAgentAtom);
        if (isBlank(cwd) || isBlank(agentId)) {
            // Sessions are stored per working directory, so without one there is nothing to
            // list rather than an error to show.
            globalStore.set(this.historyAtom, []);
            return;
        }
        try {
            const list = await this.env.rpc.AiAgentHistoryCommand(TabRpcClient, {
                connection,
                agentid: agentId,
                cwd,
            });
            globalStore.set(this.historyAtom, list ?? []);
        } catch (e) {
            globalStore.set(this.errorAtom, `Could not read past sessions: ${e?.message ?? e}`);
        }
    }

    // The id can sit anywhere in the template, so it is substituted rather than appended:
    // claude forks with --resume <id> --fork-session.
    private applySessionTemplate(template: string[], sessionId: string): string[] {
        return template.map((a) => (a == SESSION_PLACEHOLDER ? sessionId : a));
    }

    // Launches the agent in a terminal block. sessionId continues or forks a past
    // conversation, depending on mode.
    async launch(sessionId?: string, mode: "new" | "resume" | "fork" = "new"): Promise<void> {
        const agents = globalStore.get(this.agentsAtom);
        const agentId = globalStore.get(this.selectedAgentAtom);
        const agent = agents.find((a) => a.id == agentId);
        if (agent == null) {
            globalStore.set(this.errorAtom, "Pick an agent first");
            return;
        }
        if (!agent.found) {
            globalStore.set(this.errorAtom, `${agent.label} is not installed here`);
            return;
        }
        const { cwd, connection } = globalStore.get(this.targetAtom);
        // Only pass flags the selected CLI actually accepts: an unknown flag stops it from
        // starting at all, which would look like the panel being broken.
        const args: string[] = [];
        // The session template goes first: for codex resume and fork are subcommands, and a
        // subcommand has to lead the argv.
        if (!isBlank(sessionId) && mode != "new") {
            const template = mode == "fork" ? agent.forkargs : agent.resumeargs;
            if (template == null || template.length == 0) {
                globalStore.set(this.errorAtom, `${agent.label} cannot ${mode} a session`);
                return;
            }
            args.push(...this.applySessionTemplate(template, sessionId));
        }
        const permMode = globalStore.get(this.permissionModeAtom);
        if (!isBlank(permMode) && agent.permissionmodeflag) {
            args.push("--permission-mode", permMode);
        }
        const meta: MetaType = {
            view: "term",
            controller: "cmd",
            // The absolute path from detection, not the bare name: the command runs in a
            // non-login shell, so PATH additions like ~/.local/bin or nvm are not there and
            // the bare name would not resolve.
            cmd: agent.path || agent.bin,
            // Args go in cmd:args rather than the command string: a session id or a mode
            // never needs shell parsing, and quoting them would be a way to get it wrong.
            "cmd:args": args,
            "cmd:shell": false,
            "cmd:interactive": true,
            // Keep the block after the agent exits so the last output stays readable.
            "cmd:closeonexit": false,
        };
        if (!isBlank(cwd)) {
            meta["cmd:cwd"] = cwd;
        }
        if (!isBlank(connection)) {
            meta.connection = connection;
        }
        globalStore.set(this.errorAtom, null);
        try {
            await this.env.createBlock({ meta });
        } catch (e) {
            globalStore.set(this.errorAtom, `Could not start the agent: ${e?.message ?? e}`);
        }
    }
}
