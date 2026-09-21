import axios from 'axios';

const TOKEN_KEY = 'platform-token';

export const tokenStore = {
  get(): string | null {
    return localStorage.getItem(TOKEN_KEY);
  },
  set(token: string) {
    localStorage.setItem(TOKEN_KEY, token);
  },
  clear() {
    localStorage.removeItem(TOKEN_KEY);
  },
};

export const api = axios.create({
  baseURL: '/api',
  timeout: 15000,
});

api.interceptors.request.use((config) => {
  const token = tokenStore.get();
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      tokenStore.clear();
      if (window.location.pathname !== '/login') {
        window.location.href = '/login';
      }
    }
    return Promise.reject(error);
  },
);

interface Envelope<T> {
  code: number;
  message: string;
  data: T;
}

export async function unwrap<T>(promise: Promise<{ data: Envelope<T> }>): Promise<T> {
  const response = await promise;
  return response.data.data;
}
