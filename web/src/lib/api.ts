import {
  QueryClient,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { toast } from "sonner";

export class ApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      retry: (count, error) =>
        !(error instanceof ApiError && error.status < 500) && count < 1,
      refetchOnWindowFocus: true,
    },
  },
});
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const response = await fetch(`/api/admin${path}`, {
    method,
    credentials: "same-origin",
    headers:
      body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const data =
    response.status === 204 ? null : await response.json().catch(() => null);
  if (!response.ok) {
    if (response.status === 401 && path !== "/login")
      void queryClient.invalidateQueries({ queryKey: ["session"] });
    throw new ApiError(
      data?.error?.message || `请求失败（HTTP ${response.status}）`,
      response.status,
    );
  }
  if (data === null && response.status !== 204)
    throw new ApiError(
      "服务返回了无效的数据，请检查管理 API 是否正常运行。",
      response.status,
    );
  return data as T;
}
export function useAction<T, V>(
  fn: (variables: V) => Promise<T>,
  message?: string,
  onSuccess?: (data: T) => void,
) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: (data) => {
      void client.invalidateQueries();
      if (message) toast.success(message);
      onSuccess?.(data);
    },
    onError: (error) => toast.error(error.message),
  });
}
export async function copyText(value: string) {
  try {
    await navigator.clipboard.writeText(value);
    toast.success("已复制到剪贴板");
  } catch {
    toast.error("无法访问剪贴板，请手动选择并复制。");
  }
}
