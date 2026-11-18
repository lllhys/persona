// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, it, vi } from "vitest";
import { ApiError, errorText } from "../../lib/api";
import type { ConnectorPrompt } from "../../lib/connectors";

const { login, toast } = vi.hoisted(() => ({ login: vi.fn(), toast: vi.fn() }));
vi.mock("../../lib/connectors", async (original) => ({
  ...(await original<typeof import("../../lib/connectors")>()), connectorLogin: login,
}));
vi.mock("../../ui/atoms", async (original) => ({
  ...(await original<typeof import("../../ui/atoms")>()), toast,
}));
import { SignInSteps } from "./HubConnector";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

it("reports a failed first device-code request and allows retry", async () => {
  const failure = new ApiError(502, "context deadline exceeded", { code: "internal" });
  let reject!: (e: Error) => void;
  login.mockImplementationOnce(() => new Promise<ConnectorPrompt>((_, fail) => { reject = fail; }));
  login.mockResolvedValueOnce({ step: "device", kind: "device_code", user_code: "ABCD-1234", url: "https://github.com/login/device" });
  const host = document.createElement("div");
  document.body.append(host);
  const root = createRoot(host);
  try {
    await act(async () => root.render(<SignInSteps c={{ id: "github", name: "GitHub", status: "not_signed_in" }} reload={async () => {}} />));
    const start = host.querySelector("button")!;
    await act(async () => start.click());
    expect(start.disabled).toBe(true);
    await act(async () => reject(failure));
    expect(toast).toHaveBeenCalledWith(errorText(failure), "error");
    expect(start.disabled).toBe(false);
    await act(async () => start.click());
    expect(login.mock.calls).toEqual([["github", "", {}], ["github", "", {}]]);
    expect(host.textContent).toContain("ABCD-1234");
  } finally {
    act(() => root.unmount());
    host.remove();
  }
});
