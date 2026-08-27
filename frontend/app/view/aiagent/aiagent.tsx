// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import type { AiAgentViewModel } from "@/app/view/aiagent/aiagent-model";
import { useAtomValue } from "jotai";
import { memo, useEffect } from "react";

const AgentControls = memo(({ model }: { model: AiAgentViewModel }) => {
    const agents = useAtomValue(model.agentsAtom);
    const selected = useAtomValue(model.selectedAgentAtom);
    const permissionMode = useAtomValue(model.permissionModeAtom);
    const target = useAtomValue(model.targetAtom);

    const current = agents.find((a) => a.id == selected);
    const usable = agents.filter((a) => a.found && a.supported);
    const blocked = agents.filter((a) => a.found && !a.supported);
    const missing = agents.filter((a) => !a.found);

    return (
        <div className="flex flex-col gap-2 border-b border-border p-3">
            <div className="flex items-center gap-2 flex-wrap">
                <select
                    className="bg-background border border-border rounded px-2 py-1 text-xs text-primary"
                    value={selected}
                    onChange={(e) => model.updateSelected(e.target.value)}
                >
                    <option value="">Select an agent…</option>
                    {usable.map((a) => (
                        <option key={a.id} value={a.id}>
                            {a.label}
                        </option>
                    ))}
                </select>
                <select
                    className="bg-background border border-border rounded px-2 py-1 text-xs text-primary disabled:opacity-40"
                    value={permissionMode}
                    disabled={current != null && !current.permissionmodeflag}
                    title={
                        current != null && !current.permissionmodeflag
                            ? `${current.label} manages permissions itself`
                            : "Passed as --permission-mode"
                    }
                    onChange={(e) => model.updatePermissionMode(e.target.value)}
                >
                    <option value="">permissions: default</option>
                    {/* The modes come from the catalog so the list cannot drift from what
                        the CLI accepts. */}
                    {(current?.permissionmodes ?? []).map((m) => (
                        <option key={m} value={m}>
                            {m == "manual" ? "manual (denies the rest)" : m}
                        </option>
                    ))}
                </select>
                <button
                    className="px-3 py-1 text-xs rounded bg-accent text-background hover:opacity-80 cursor-pointer disabled:opacity-50"
                    disabled={selected == ""}
                    onClick={() => model.launch()}
                >
                    New session
                </button>
                <button
                    className="px-2 py-1 text-xs rounded border border-border hover:bg-secondary/50 cursor-pointer"
                    onClick={() => model.refresh()}
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
AgentControls.displayName = "AgentControls";

export const AiAgentView = memo(({ model }: ViewComponentProps<AiAgentViewModel>) => {
    const history = useAtomValue(model.historyAtom);
    const error = useAtomValue(model.errorAtom);
    const isLoading = useAtomValue(model.isLoadingAtom);
    const target = useAtomValue(model.targetAtom);
    const agents = useAtomValue(model.agentsAtom);
    const selectedId = useAtomValue(model.selectedAgentAtom);
    // Past sessions come from the claude store and are reopened with --resume, so they are
    // only actionable for an agent that takes that flag.
    const selectedAgent = agents.find((a) => a.id == selectedId);
    const canList = selectedAgent?.historysupported ?? false;
    const canFork = (selectedAgent?.forkargs?.length ?? 0) > 0;

    // The tab's project can appear or change after the panel mounts. A connection change
    // needs a full re-detect (the agents live on the other machine); a directory change only
    // moves the session store.
    useEffect(() => {
        model.refresh();
    }, [target.connection]);
    useEffect(() => {
        model.loadHistory();
    }, [target.cwd]);

    return (
        <div className="flex flex-col h-full min-h-0">
            <AgentControls model={model} />
            {error != null && <div className="px-3 py-2 text-xs text-error">{error}</div>}
            <div className="flex-1 min-h-0 overflow-auto p-3 flex flex-col gap-1">
                <div className="text-[10px] uppercase tracking-wide text-secondary">Past sessions</div>
                {isLoading && <div className="text-xs text-secondary">Looking…</div>}
                {!isLoading && !canList && (
                    <div className="text-xs text-secondary">
                        {selectedAgent?.note ?? "The selected agent keeps its own session history."}
                    </div>
                )}
                {!isLoading && canList && history.length == 0 && (
                    <div className="text-xs text-secondary">
                        {target.cwd == ""
                            ? "Open this panel in a project tab to see its sessions."
                            : "No sessions yet for this directory."}
                    </div>
                )}
                {canList &&
                    history.map((h) => (
                        <div key={h.sessionid} className="group flex items-center gap-1">
                            <button
                                className="flex-1 min-w-0 text-left text-xs text-primary hover:bg-hoverbg rounded px-1 py-1 cursor-pointer truncate"
                                title={`resume ${h.sessionid}`}
                                onClick={() => model.launch(h.sessionid, "resume")}
                            >
                                {h.title || h.sessionid}
                            </button>
                            {/* Forking starts a new session from this point and leaves the
                                original untouched. */}
                            {canFork && (
                                <button
                                    className="shrink-0 px-1.5 py-0.5 text-[10px] rounded border border-border text-secondary hover:bg-secondary/50 cursor-pointer opacity-0 group-hover:opacity-100 transition-opacity"
                                    title="Fork into a new session, keeping the original"
                                    onClick={() => model.launch(h.sessionid, "fork")}
                                >
                                    fork
                                </button>
                            )}
                        </div>
                    ))}
            </div>
            <div className="border-t border-border px-3 py-2 text-[10px] text-secondary">
                Sessions open as terminal blocks, so permission prompts, slash commands and Ctrl+C work as they do in
                the CLI.
            </div>
        </div>
    );
});
AiAgentView.displayName = "AiAgentView";
