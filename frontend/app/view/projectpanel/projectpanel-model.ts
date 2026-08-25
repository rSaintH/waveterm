// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { BlockNodeModel } from "@/app/block/blocktypes";
import { globalStore } from "@/app/store/jotaiStore";
import type { TabModel } from "@/app/store/tab-model";
import { TabRpcClient } from "@/app/store/wshrpcutil";
import { ProjectPanelView } from "@/app/view/projectpanel/projectpanel";
import type { ProjectPanelEnv } from "@/app/view/projectpanel/projectpanelenv";
import { atom, type Atom, type PrimitiveAtom } from "jotai";

export type ProjectEntry = ProjectConfigType & { key: string };

export type ProjectForm = {
    key: string;
    label: string;
    path: string;
    connection: string;
    repourl: string;
    produrl: string;
    description: string;
    icon: string;
    color: string;
    order: string;
    hidden: boolean;
};

export function emptyProjectForm(): ProjectForm {
    return {
        key: "",
        label: "",
        path: "",
        connection: "",
        repourl: "",
        produrl: "",
        description: "",
        icon: "",
        color: "",
        order: "",
        hidden: false,
    };
}

function formFromEntry(entry: ProjectEntry): ProjectForm {
    return {
        key: entry.key,
        label: entry.label ?? "",
        path: entry.path ?? "",
        connection: entry.connection ?? "",
        repourl: entry.repourl ?? "",
        produrl: entry.produrl ?? "",
        description: entry.description ?? "",
        icon: entry.icon ?? "",
        color: entry.color ?? "",
        order: entry["display:order"] == null ? "" : String(entry["display:order"]),
        hidden: entry["display:hidden"] ?? false,
    };
}

// Only non-empty fields are written, so a project entry stays as small as the user made it.
function metaFromForm(form: ProjectForm): { [key: string]: any } {
    const meta: { [key: string]: any } = { path: form.path.trim() };
    const putString = (key: string, val: string) => {
        const trimmed = val.trim();
        if (trimmed !== "") {
            meta[key] = trimmed;
        }
    };
    putString("label", form.label);
    putString("connection", form.connection);
    putString("repourl", form.repourl);
    putString("produrl", form.produrl);
    putString("description", form.description);
    putString("icon", form.icon);
    putString("color", form.color);
    const order = parseFloat(form.order);
    if (Number.isFinite(order)) {
        meta["display:order"] = order;
    }
    if (form.hidden) {
        meta["display:hidden"] = true;
    }
    return meta;
}

export function validateProjectForm(form: ProjectForm, existingKeys: string[], originalKey: string | null): string {
    const key = form.key.trim();
    if (key === "") {
        return "Key is required";
    }
    if (key !== originalKey && existingKeys.includes(key)) {
        return `A project named "${key}" already exists`;
    }
    if (form.path.trim() === "") {
        return "Path is required";
    }
    return null;
}

export class ProjectPanelViewModel implements ViewModel {
    blockId: string;
    viewType = "projectpanel";
    viewIcon = atom("diagram-project");
    viewName = atom("Projects");
    viewComponent = ProjectPanelView;
    noPadding = atom(true);
    nodeModel: BlockNodeModel;
    tabModel: TabModel;
    env: ProjectPanelEnv;

    projectsAtom: Atom<ProjectEntry[]>;
    // null when the form is closed, "" when adding, otherwise the key being edited.
    editingKeyAtom: PrimitiveAtom<string | null>;
    formAtom: PrimitiveAtom<ProjectForm>;
    errorAtom: PrimitiveAtom<string>;
    isSavingAtom: PrimitiveAtom<boolean>;

    constructor({ blockId, nodeModel, tabModel, waveEnv }: ViewModelInitType) {
        this.blockId = blockId;
        this.nodeModel = nodeModel;
        this.tabModel = tabModel;
        this.env = waveEnv as ProjectPanelEnv;

        this.editingKeyAtom = atom(null) as PrimitiveAtom<string | null>;
        this.formAtom = atom(emptyProjectForm());
        this.errorAtom = atom(null) as PrimitiveAtom<string>;
        this.isSavingAtom = atom(false);

        // Hidden projects are shown here on purpose: this panel is where you unhide them.
        this.projectsAtom = atom((get) => {
            const fullConfig = get(this.env.atoms.fullConfigAtom);
            const projects = fullConfig?.projects ?? {};
            return Object.entries(projects)
                .map(([key, proj]) => ({ ...proj, key }))
                .sort(
                    (a, b) =>
                        (a["display:order"] ?? 0) - (b["display:order"] ?? 0) ||
                        (a.label ?? a.key).localeCompare(b.label ?? b.key)
                );
        });
    }

    startAdd() {
        globalStore.set(this.formAtom, emptyProjectForm());
        globalStore.set(this.errorAtom, null);
        globalStore.set(this.editingKeyAtom, "");
    }

    startEdit(entry: ProjectEntry) {
        globalStore.set(this.formAtom, formFromEntry(entry));
        globalStore.set(this.errorAtom, null);
        globalStore.set(this.editingKeyAtom, entry.key);
    }

    cancelEdit() {
        globalStore.set(this.editingKeyAtom, null);
        globalStore.set(this.errorAtom, null);
    }

    updateForm(patch: Partial<ProjectForm>) {
        globalStore.set(this.formAtom, { ...globalStore.get(this.formAtom), ...patch });
    }

    async saveForm(): Promise<void> {
        const form = globalStore.get(this.formAtom);
        const originalKey = globalStore.get(this.editingKeyAtom);
        const existingKeys = globalStore.get(this.projectsAtom).map((p) => p.key);
        const validationError = validateProjectForm(form, existingKeys, originalKey === "" ? null : originalKey);
        if (validationError != null) {
            globalStore.set(this.errorAtom, validationError);
            return;
        }
        const newKey = form.key.trim();
        globalStore.set(this.isSavingAtom, true);
        globalStore.set(this.errorAtom, null);
        try {
            await this.env.rpc.SetProjectConfigCommand(TabRpcClient, {
                projectkey: newKey,
                metamaptype: metaFromForm(form),
            });
            // Renaming a project writes the new key first, so a failure here leaves the
            // original entry intact rather than losing the project entirely.
            if (originalKey && originalKey !== newKey) {
                await this.env.rpc.DeleteProjectConfigCommand(TabRpcClient, originalKey);
            }
            globalStore.set(this.editingKeyAtom, null);
        } catch (e) {
            globalStore.set(this.errorAtom, `Could not save: ${e?.message ?? e}`);
        } finally {
            globalStore.set(this.isSavingAtom, false);
        }
    }

    async deleteProject(key: string): Promise<void> {
        globalStore.set(this.errorAtom, null);
        try {
            await this.env.rpc.DeleteProjectConfigCommand(TabRpcClient, key);
            if (globalStore.get(this.editingKeyAtom) === key) {
                globalStore.set(this.editingKeyAtom, null);
            }
        } catch (e) {
            globalStore.set(this.errorAtom, `Could not delete: ${e?.message ?? e}`);
        }
    }

    openProjectTab(entry: ProjectEntry) {
        if (!entry.path) {
            return;
        }
        this.env.electron.createTab({
            tabName: entry.label || entry.key,
            cwd: entry.path,
            connection: entry.connection,
            projectKey: entry.key,
        });
    }

    // Opens the native directory picker, seeded with whatever is already typed in the form.
    // Also fills in an empty key/label from the folder name, since that is almost always
    // what you want and is still editable afterwards.
    async browseForPath(): Promise<void> {
        const form = globalStore.get(this.formAtom);
        try {
            const picked = await this.env.electron.selectDirectory(form.path.trim() || undefined);
            if (!picked) {
                return;
            }
            // Browsing always picks a local Windows folder, so a project previously pointed at a
            // connection would be left with a mismatched path; clear it to keep the two in sync.
            const patch: Partial<ProjectForm> = { path: picked, connection: "" };
            const folderName = picked.split(/[\\/]/).filter(Boolean).pop();
            if (folderName) {
                if (form.key.trim() === "") {
                    patch.key = folderName;
                }
                if (form.label.trim() === "") {
                    patch.label = folderName;
                }
            }
            this.updateForm(patch);
        } catch (e) {
            globalStore.set(this.errorAtom, `Could not open the directory picker: ${e?.message ?? e}`);
        }
    }

    openUrl(url: string) {
        if (!url) {
            return;
        }
        this.env.electron.openExternal(url);
    }
}
