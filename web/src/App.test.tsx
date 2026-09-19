import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { api, queryClient } from "@/lib/api";
import App from "./App";

vi.mock("@/pages/overview", () => ({
  OverviewPage: () => (
    <div>
      已登录的工作空间
      <button onClick={() => void api("/overview").catch(() => undefined)}>
        请求受保护数据
      </button>
    </div>
  ),
}));

let authenticated = false;
let expired = false;
let setupRequired = false;
const fetchMock = vi.fn(
  async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input);
    if (path.endsWith("/session"))
      return Response.json({
        authenticated,
        setupRequired,
        username: authenticated ? "admin" : "",
      });
    if (path.endsWith("/login")) {
      const body = JSON.parse(String(init?.body));
      if (body.password !== "correct-password")
        return Response.json(
          { error: { message: "密码错误" } },
          { status: 401 },
        );
      authenticated = true;
      return Response.json({ ok: true });
    }
    if (path.endsWith("/setup")) {
      authenticated = true;
      setupRequired = false;
      return Response.json({ ok: true });
    }
    if (path.endsWith("/overview") && expired) {
      authenticated = false;
      return Response.json(
        { error: { message: "Session expired" } },
        { status: 401 },
      );
    }
    throw new Error(`Unexpected request: ${path}`);
  },
);
beforeEach(() => {
  authenticated = false;
  expired = false;
  setupRequired = false;
  window.location.hash = "";
  queryClient.clear();
  fetchMock.mockClear();
  vi.stubGlobal("fetch", fetchMock);
  vi.stubGlobal("scrollTo", vi.fn());
});
afterEach(() => {
  cleanup();
  queryClient.clear();
  vi.unstubAllGlobals();
});

describe("administrator session flow", () => {
  it("re-fetches the observed session after login and opens the workspace", async () => {
    render(
      <QueryClientProvider client={queryClient}>
        <App />
      </QueryClientProvider>,
    );
    expect(
      await screen.findByRole("heading", { name: "登录管理控制台" }),
    ).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("密码"), {
      target: { value: "correct-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "登录控制台" }));
    expect(await screen.findByText("已登录的工作空间")).toBeInTheDocument();
    expect(
      fetchMock.mock.calls.filter(([url]) => String(url).endsWith("/session")),
    ).toHaveLength(2);
    expect(window.localStorage.length).toBe(0);
  });
  it("returns to login when a protected request reports an expired session", async () => {
    authenticated = true;
    render(
      <QueryClientProvider client={queryClient}>
        <App />
      </QueryClientProvider>,
    );
    expect(await screen.findByText("已登录的工作空间")).toBeInTheDocument();
    expired = true;
    fireEvent.click(screen.getByRole("button", { name: "请求受保护数据" }));
    expect(
      await screen.findByRole("heading", { name: "登录管理控制台" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("已登录的工作空间")).not.toBeInTheDocument();
  });
  it("keeps a rejected login actionable and shows the actual server error", async () => {
    render(
      <QueryClientProvider client={queryClient}>
        <App />
      </QueryClientProvider>,
    );
    await screen.findByLabelText("密码");
    fireEvent.change(screen.getByLabelText("密码"), {
      target: { value: "wrong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "登录控制台" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("密码错误");
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "登录控制台" }),
      ).not.toBeDisabled(),
    );
  });
  it("requires matching passwords before initialization", async () => {
    setupRequired = true;
    render(
      <QueryClientProvider client={queryClient}>
        <App />
      </QueryClientProvider>,
    );
    await screen.findByRole("heading", { name: "初始化你的网关" });
    expect(screen.queryByLabelText("初始化令牌")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("密码"), {
      target: { value: "a-long-password" },
    });
    fireEvent.change(screen.getByLabelText("确认密码"), {
      target: { value: "different-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建账户并进入" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "两次输入的密码不一致",
    );
    expect(
      fetchMock.mock.calls.some(([url]) => String(url).endsWith("/setup")),
    ).toBe(false);
  });
  it("initializes with only a username and an eight-character password", async () => {
    setupRequired = true;
    render(
      <QueryClientProvider client={queryClient}>
        <App />
      </QueryClientProvider>,
    );
    await screen.findByRole("heading", { name: "初始化你的网关" });
    expect(screen.queryByLabelText("初始化令牌")).not.toBeInTheDocument();
    expect(screen.getByLabelText("密码")).toHaveAttribute("minlength", "8");
    fireEvent.change(screen.getByLabelText("密码"), {
      target: { value: "Test1234" },
    });
    fireEvent.change(screen.getByLabelText("确认密码"), {
      target: { value: "Test1234" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建账户并进入" }));
    expect(await screen.findByText("已登录的工作空间")).toBeInTheDocument();
    const setupCall = fetchMock.mock.calls.find(([url]) =>
      String(url).endsWith("/setup"),
    );
    expect(JSON.parse(String(setupCall?.[1]?.body))).toEqual({
      username: "admin",
      password: "Test1234",
    });
  });
});
