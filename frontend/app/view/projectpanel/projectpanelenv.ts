// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import type { WaveEnv, WaveEnvSubset } from "@/app/waveenv/waveenv";

export type ProjectPanelEnv = WaveEnvSubset<{
    electron: {
        createTab: WaveEnv["electron"]["createTab"];
        openExternal: WaveEnv["electron"]["openExternal"];
        selectDirectory: WaveEnv["electron"]["selectDirectory"];
    };
    rpc: {
        SetProjectConfigCommand: WaveEnv["rpc"]["SetProjectConfigCommand"];
        DeleteProjectConfigCommand: WaveEnv["rpc"]["DeleteProjectConfigCommand"];
    };
    atoms: {
        fullConfigAtom: WaveEnv["atoms"]["fullConfigAtom"];
    };
}>;
