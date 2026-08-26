// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { Markdown } from "@/app/element/markdown";
import type { AiAgentViewModel, ChatEntry } from "@/app/view/aiagent/aiagent-model";
import { cn } from "@/util/util";
import { useAtomValue } from "jotai";
import { memo, useEffect, useRef } from "react";

const roleStyle: { [k in ChatEntry["role"]]: string } = {
    you: "text-primary",
    agent: "text-primary",
    tools: "text-secondary italic",
    status: "text-secondary",
    error: "text-error",
};

const roleLabel: { [k in ChatEntry["role"]]: string } = {
    you: "you",
    agent: "agent",
    tools: "tools",
    status: "·",
    error: "error",
};

const Entry = memo(({ entry }: { entry: ChatEntry }) => (
    <div className="flex gap-2 text-xs leading-relaxed">
        <span className="shrink-0 w-12 text-right text-secondary select-none">{roleLabel[entry.role]}</span>
        {/* Only the agent writes markdown; echoing a status line through the renderer would
            mangle paths and punctuation. */}
        {entry.role == "agent" ? (
            <div className="min-w-0 flex-1">
                <Markdown text={entry.text} />
            </div>
        ) : (
            <span className={cn("whitespace-pre-wrap break-words min-w-0", roleStyle[entry.role])}>{entry.text}</span>
        )}
    </div>
));
Entry.displayName = "Entry";

const AgentPicker = memo(({ model }: { model: AiAgentViewModel }) => {
    const agents = useAtomValue(model.agentsAtom);
    const selected = useAtomValue(model.selectedAgentAtom);
    const status = useAtomValue(model.statusAtom);
    const target = useAtomValue(model.targetAtom);
    const permissionMode = useAtomValue(model.permissionModeAtom);
    const running = status == "running" || status == "starting";

    const usable = agents.filter((a) => a.found && a.supported);
    const blocked = agents.filter((a) => a.found && !a.supported);
    const missing = agents.filter((a) => !a.found);

    return (
        <div className="flex flex-col gap-2 border-b border-border p-3">
            <div className="flex items-center gap-2 flex-wrap">
                <select
                    className="bg-background border border-border rounded px-2 py-1 text-xs text-primary"
                    value={selected}
                    disabled={running}
                    onChange={(e) => model.updateSelected(e.target.value)}
                >
                    <option value="">Select an agent…</option>
                    {usable.map((a) => (
                        <option key={a.id} value={a.id}>
                            {a.label}
                        </option>
                    ))}
                </select>
                {!running && (
                    <button
                        className="px-3 py-1 text-xs rounded bg-accent text-background hover:opacity-80 cursor-pointer disabled:opacity-50"
                        disabled={selected == ""}
                        onClick={() => model.start()}
                    >
                        Start
                    </button>
                )}
                {running && (
                    <button
                        className="px-3 py-1 text-xs rounded border border-border hover:bg-secondary/50 cursor-pointer"
                        onClick={() => model.stop()}
                    >
                        Stop
                    </button>
                )}
                {running && (
                    <button
                        className="px-3 py-1 text-xs rounded border border-border hover:bg-secondary/50 cursor-pointer"
                        title="Cancel the current turn but keep the session"
                        onClick={() => model.interrupt()}
                    >
                        Interrupt
                    </button>
                )}
                <select
                    className="bg-background border border-border rounded px-2 py-1 text-xs text-primary"
                    value={permissionMode}
                    title="Permission mode"
                    onChange={(e) => model.setPermissionMode(e.target.value)}
                >
                    <option value="">permissions: default</option>
                    <option value="manual">ask (manual)</option>
                    <option value="auto">auto</option>
                    <option value="acceptEdits">accept edits</option>
                    <option value="plan">plan</option>
                    <option value="dontAsk">don&apos;t ask</option>
                    <option value="bypassPermissions">bypass all</option>
                </select>
                <button
                    className="px-2 py-1 text-xs rounded border border-border hover:bg-secondary/50 cursor-pointer"
                    onClick={() => model.loadAgents()}
                >
                    Rescan
                </button>
            </div>
            <div className="text-[11px] text-secondary">
                {target.connection ? `runs on ${target.connection}` : "runs locally"}
                {target.cwd ? ` · ${target.cwd}` : " · no project directory"}
            </div>
            {/* Installed-but-undriveable agents are listed with the reason instead of hidden,
                so "I have it installed and it is not here" never happens silently. */}
            {blocked.map((a) => (
                <div key={a.id} className="text-[11px] text-secondary">
                    {a.label}: {a.note}
                </div>
            ))}
            {usable.length == 0 && missing.length > 0 && (
                <div className="text-[11px] text-secondary">
                    Not installed here: {missing.map((a) => a.label).join(", ")}
                </div>
            )}
        </div>
    );
});
AgentPicker.displayName = "AgentPicker";

const ToolApproval = memo(({ model }: { model: AiAgentViewModel }) => {
    const pending = useAtomValue(model.pendingToolAtom);
    if (pending == null) {
        return null;
    }
    return (
        <div className="border-t border-accent bg-accent/10 p-3 flex flex-col gap-2">
            <div className="text-xs font-semibold">
                The agent wants to use <span className="text-accent">{pending.toolName}</span>
            </div>
            {pending.input != "" && (
                <pre className="text-[11px] text-secondary max-h-32 overflow-auto whitespace-pre-wrap break-words">
                    {pending.input}
                </pre>
            )}
            <div className="flex gap-2 justify-end">
                <button
                    className="px-3 py-1 text-xs rounded border border-border hover:bg-secondary/50 cursor-pointer"
                    onClick={() => model.decideTool(false)}
                >
                    Deny
                </button>
                <button
                    className="px-3 py-1 text-xs rounded bg-accent text-background hover:opacity-80 cursor-pointer"
                    onClick={() => model.decideTool(true)}
                >
                    Allow
                </button>
            </div>
        </div>
    );
});
ToolApproval.displayName = "ToolApproval";

const HistoryList = memo(({ model }: { model: AiAgentViewModel }) => {
    const history = useAtomValue(model.historyAtom);
    const status = useAtomValue(model.statusAtom);
    if (history.length == 0 || status == "running" || status == "starting") {
        return null;
    }
    return (
        <div className="flex flex-col gap-1 p-3 border-b border-border">
            <div className="text-[10px] uppercase tracking-wide text-secondary">Past sessions</div>
            {history.map((h) => (
                <button
                    key={h.sessionid}
                    className="text-left text-xs text-primary hover:bg-hoverbg rounded px-1 py-0.5 cursor-pointer truncate"
                    title={h.sessionid}
                    onClick={() => model.start(h.sessionid)}
                >
                    {h.title || h.sessionid}
                </button>
            ))}
        </div>
    );
});
HistoryList.displayName = "HistoryList";

export const AiAgentView = memo(({ model }: ViewComponentProps<AiAgentViewModel>) => {
    const entries = useAtomValue(model.entriesAtom);
    const status = useAtomValue(model.statusAtom);
    const input = useAtomValue(model.inputAtom);
    const error = useAtomValue(model.errorAtom);
    const cost = useAtomValue(model.costAtom);
    const scrollRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        model.loadAgents();
        model.loadHistory();
    }, []);

    // Follow the conversation as it grows; an agent turn can be long.
    useEffect(() => {
        const el = scrollRef.current;
        if (el != null) {
            el.scrollTop = el.scrollHeight;
        }
    }, [entries.length]);

    const running = status == "running";

    return (
        <div className="flex flex-col h-full min-h-0">
            <AgentPicker model={model} />
            <HistoryList model={model} />
            <div ref={scrollRef} className="flex-1 min-h-0 overflow-auto flex flex-col gap-1 p-3">
                {entries.length == 0 && (
                    <div className="text-xs text-secondary">
                        Start a session to talk to a coding agent in this project.
                    </div>
                )}
                {entries.map((e) => (
                    <Entry key={e.id} entry={e} />
                ))}
            </div>
            <ToolApproval model={model} />
            {error != null && <div className="px-3 pb-2 text-xs text-error">{error}</div>}
            <div className="flex items-center gap-2 border-t border-border p-2">
                <input
                    className="flex-1 bg-background border border-border rounded px-2 py-1 text-xs text-primary focus:outline-none focus:border-accent disabled:opacity-50"
                    placeholder={running ? "Message the agent…" : "Start a session first"}
                    value={input}
                    disabled={!running}
                    onChange={(e) => model.updateInput(e.target.value)}
                    onKeyDown={(e) => {
                        if (e.key == "Enter" && !e.shiftKey) {
                            e.preventDefault();
                            model.send();
                        }
                    }}
                />
                <button
                    className="px-3 py-1 text-xs rounded border border-border hover:bg-secondary/50 cursor-pointer disabled:opacity-50"
                    disabled={!running || input.trim() == ""}
                    onClick={() => model.send()}
                >
                    Send
                </button>
            </div>
            <div className="flex justify-between px-3 pb-2 text-[10px] text-secondary">
                <span>{status}</span>
                {/* Cost is shown because an agent session can get expensive quietly. */}
                {cost > 0 && <span>${cost.toFixed(4)}</span>}
            </div>
        </div>
    );
});
AiAgentView.displayName = "AiAgentView";
