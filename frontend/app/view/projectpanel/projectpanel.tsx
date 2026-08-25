// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import type { ProjectEntry, ProjectForm, ProjectPanelViewModel } from "@/app/view/projectpanel/projectpanel-model";
import { cn } from "@/util/util";
import { useAtomValue } from "jotai";
import { memo, useState } from "react";

const inputClass =
    "w-full bg-background border border-border rounded px-2 py-1 text-xs text-primary focus:outline-none focus:border-accent";

type FieldProps = {
    label: string;
    value: string;
    placeholder?: string;
    onChange: (val: string) => void;
};

const Field = memo(({ label, value, placeholder, onChange }: FieldProps) => (
    <label className="flex flex-col gap-1">
        <span className="text-[10px] uppercase tracking-wide text-secondary">{label}</span>
        <input
            className={inputClass}
            value={value}
            placeholder={placeholder}
            onChange={(e) => onChange(e.target.value)}
        />
    </label>
));
Field.displayName = "Field";

type ProjectFormProps = {
    model: ProjectPanelViewModel;
    form: ProjectForm;
    isNew: boolean;
};

const ProjectFormPanel = memo(({ model, form, isNew }: ProjectFormProps) => {
    const error = useAtomValue(model.errorAtom);
    const isSaving = useAtomValue(model.isSavingAtom);

    return (
        <div className="flex flex-col gap-2 border border-border rounded p-3 bg-secondary/10">
            <div className="text-xs font-semibold">{isNew ? "New project" : `Editing "${form.key}"`}</div>
            <div className="grid grid-cols-2 gap-2">
                <Field label="Key" value={form.key} onChange={(v) => model.updateForm({ key: v })} />
                <Field label="Label" value={form.label} onChange={(v) => model.updateForm({ label: v })} />
            </div>
            <div className="flex flex-col gap-1">
                <span className="text-[10px] uppercase tracking-wide text-secondary">Path</span>
                <div className="flex gap-2">
                    <input
                        className={inputClass}
                        value={form.path}
                        placeholder="C:/dev/meu-projeto"
                        onChange={(e) => model.updateForm({ path: e.target.value })}
                    />
                    <button
                        className="shrink-0 px-2 py-1 text-xs rounded border border-border hover:bg-secondary/50 cursor-pointer"
                        onClick={() => model.browseForPath()}
                    >
                        Browse…
                    </button>
                </div>
            </div>
            <Field
                label="Connection"
                value={form.connection}
                placeholder="vazio = maquina local · wsl://Ubuntu · user@host"
                onChange={(v) => model.updateForm({ connection: v })}
            />
            <div className="grid grid-cols-2 gap-2">
                <Field label="Repo URL" value={form.repourl} onChange={(v) => model.updateForm({ repourl: v })} />
                <Field label="Prod URL" value={form.produrl} onChange={(v) => model.updateForm({ produrl: v })} />
            </div>
            <Field
                label="Description"
                value={form.description}
                onChange={(v) => model.updateForm({ description: v })}
            />
            <div className="grid grid-cols-3 gap-2">
                <Field
                    label="Icon"
                    value={form.icon}
                    placeholder="code-branch"
                    onChange={(v) => model.updateForm({ icon: v })}
                />
                <Field label="Color" value={form.color} placeholder="#58c7f3" onChange={(v) => model.updateForm({ color: v })} />
                <Field label="Order" value={form.order} placeholder="1" onChange={(v) => model.updateForm({ order: v })} />
            </div>
            <label className="flex items-center gap-2 text-xs text-secondary">
                <input
                    type="checkbox"
                    checked={form.hidden}
                    onChange={(e) => model.updateForm({ hidden: e.target.checked })}
                />
                Hide from the new-tab menu
            </label>
            {error != null && <div className="text-xs text-error">{error}</div>}
            <div className="flex gap-2 justify-end">
                <button
                    className="px-3 py-1 text-xs rounded border border-border hover:bg-secondary/50 cursor-pointer"
                    onClick={() => model.cancelEdit()}
                >
                    Cancel
                </button>
                <button
                    className="px-3 py-1 text-xs rounded bg-accent text-background hover:opacity-80 cursor-pointer disabled:opacity-50"
                    disabled={isSaving}
                    onClick={() => model.saveForm()}
                >
                    {isSaving ? "Saving…" : "Save"}
                </button>
            </div>
        </div>
    );
});
ProjectFormPanel.displayName = "ProjectFormPanel";

type ProjectRowProps = {
    model: ProjectPanelViewModel;
    entry: ProjectEntry;
};

const ProjectRow = memo(({ model, entry }: ProjectRowProps) => {
    const [confirmingDelete, setConfirmingDelete] = useState(false);

    return (
        <div className="flex flex-col gap-1 border border-border rounded p-2 hover:bg-secondary/20 transition-colors">
            <div className="flex items-center gap-2">
                <i
                    className={cn("fa fa-solid shrink-0", entry.icon ? `fa-${entry.icon}` : "fa-diagram-project")}
                    style={entry.color ? { color: entry.color } : undefined}
                />
                <span className="text-xs font-semibold truncate">{entry.label || entry.key}</span>
                {entry["display:hidden"] && (
                    <span className="text-[10px] px-1 rounded bg-secondary/40 text-secondary shrink-0">hidden</span>
                )}
            </div>
            <div className="text-[11px] text-secondary truncate" title={entry.path}>
                {entry.connection ? `${entry.connection} · ${entry.path}` : entry.path}
            </div>
            {entry.description && <div className="text-[11px] text-secondary truncate">{entry.description}</div>}
            <div className="flex flex-wrap gap-1 pt-1">
                <button
                    className="px-2 py-0.5 text-[11px] rounded border border-border hover:bg-secondary/50 cursor-pointer"
                    onClick={() => model.openProjectTab(entry)}
                >
                    Open tab
                </button>
                {entry.repourl && (
                    <button
                        className="px-2 py-0.5 text-[11px] rounded border border-border hover:bg-secondary/50 cursor-pointer"
                        onClick={() => model.openUrl(entry.repourl)}
                    >
                        Repo
                    </button>
                )}
                {entry.produrl && (
                    <button
                        className="px-2 py-0.5 text-[11px] rounded border border-border hover:bg-secondary/50 cursor-pointer"
                        onClick={() => model.openUrl(entry.produrl)}
                    >
                        Prod
                    </button>
                )}
                <button
                    className="px-2 py-0.5 text-[11px] rounded border border-border hover:bg-secondary/50 cursor-pointer"
                    onClick={() => model.startEdit(entry)}
                >
                    Edit
                </button>
                {confirmingDelete ? (
                    <>
                        <button
                            className="px-2 py-0.5 text-[11px] rounded border border-error text-error hover:bg-error/20 cursor-pointer"
                            onClick={() => {
                                setConfirmingDelete(false);
                                model.deleteProject(entry.key);
                            }}
                        >
                            Confirm delete
                        </button>
                        <button
                            className="px-2 py-0.5 text-[11px] rounded border border-border hover:bg-secondary/50 cursor-pointer"
                            onClick={() => setConfirmingDelete(false)}
                        >
                            Keep
                        </button>
                    </>
                ) : (
                    <button
                        className="px-2 py-0.5 text-[11px] rounded border border-border text-secondary hover:bg-secondary/50 cursor-pointer"
                        onClick={() => setConfirmingDelete(true)}
                    >
                        Delete
                    </button>
                )}
            </div>
        </div>
    );
});
ProjectRow.displayName = "ProjectRow";

export const ProjectPanelView = memo(({ model }: ViewComponentProps<ProjectPanelViewModel>) => {
    const projects = useAtomValue(model.projectsAtom);
    const editingKey = useAtomValue(model.editingKeyAtom);
    const form = useAtomValue(model.formAtom);
    const error = useAtomValue(model.errorAtom);

    return (
        <div className="flex flex-col gap-2 h-full overflow-auto p-3">
            <div className="flex items-center justify-between">
                <span className="text-xs text-secondary">
                    {projects.length} project{projects.length === 1 ? "" : "s"}
                </span>
                <button
                    className="px-2 py-0.5 text-[11px] rounded border border-border hover:bg-secondary/50 cursor-pointer"
                    onClick={() => model.startAdd()}
                >
                    + New project
                </button>
            </div>

            {editingKey != null && <ProjectFormPanel model={model} form={form} isNew={editingKey === ""} />}
            {editingKey == null && error != null && <div className="text-xs text-error">{error}</div>}

            {projects.length === 0 && editingKey == null && (
                <div className="text-xs text-secondary py-4 text-center">
                    No projects yet. Add one to open tabs whose terminal starts in a fixed directory.
                </div>
            )}

            <div className="flex flex-col gap-2">
                {projects.map((entry) => (
                    <ProjectRow key={entry.key} model={model} entry={entry} />
                ))}
            </div>
        </div>
    );
});
ProjectPanelView.displayName = "ProjectPanelView";
