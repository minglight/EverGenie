import { renameBrowserStorage } from "./storageRename";

// main.tsx 第一個 import 這支：其他模組載入（可能一載入就讀偏好）之前先把舊鍵搬好。
renameBrowserStorage();
