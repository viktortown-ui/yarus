package ru.viktortown.yarus;

import org.json.JSONObject;

import java.io.BufferedInputStream;
import java.io.ByteArrayOutputStream;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.Inet4Address;
import java.net.InetAddress;
import java.net.NetworkInterface;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Enumeration;
import java.util.HashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/** Small local HTTP gateway. Business rules stay in the shared JavaScript host engine.
 * The gateway is alive only while MainActivity is alive, which is shown explicitly in the UI.
 */
public final class MobileHostServer {
    private final MainActivity activity;
    private final ExecutorService clients = Executors.newFixedThreadPool(4);
    private volatile ServerSocket server;
    private volatile Thread acceptThread;
    private volatile int port;

    public MobileHostServer(MainActivity activity) { this.activity = activity; }

    public synchronized String start() throws Exception {
        if (server != null && !server.isClosed()) return addressesJson();
        Exception last = null;
        for (int candidate = 8787; candidate <= 8797; candidate++) {
            try {
                ServerSocket socket = new ServerSocket(candidate, 20);
                socket.setReuseAddress(true);
                server = socket;
                port = candidate;
                break;
            } catch (Exception error) { last = error; }
        }
        if (server == null) throw new IllegalStateException("Порты 8787–8797 заняты.", last);
        acceptThread = new Thread(this::acceptLoop, "yarus-mobile-host");
        acceptThread.setDaemon(true);
        acceptThread.start();
        return addressesJson();
    }

    public synchronized void stop() {
        ServerSocket current = server;
        server = null;
        if (current != null) try { current.close(); } catch (Exception ignored) {}
        Thread thread = acceptThread;
        acceptThread = null;
        if (thread != null) thread.interrupt();
    }

    public synchronized void destroy() {
        stop();
        clients.shutdownNow();
    }

    public synchronized boolean isRunning() { return server != null && !server.isClosed(); }

    private void acceptLoop() {
        while (isRunning()) {
            try {
                Socket socket = server.accept();
                clients.execute(() -> handle(socket));
            } catch (Exception error) {
                if (isRunning()) activity.reportHostError("Сервер телефона остановлен: " + error.getMessage());
            }
        }
    }

    private void handle(Socket socket) {
        try (Socket client = socket) {
            client.setSoTimeout(25000);
            BufferedInputStream input = new BufferedInputStream(client.getInputStream());
            String first = readLine(input, 4096);
            if (first == null) return;
            String[] requestLine = first.split(" ");
            if (requestLine.length != 3) { send(client, 400, "{\"error\":\"Некорректный HTTP-запрос.\"}", ""); return; }
            String method = requestLine[0].toUpperCase(Locale.ROOT);
            String path = requestLine[1];
            if (!path.startsWith("/api/") || path.length() > 160 || (!method.equals("GET") && !method.equals("POST") && !method.equals("OPTIONS"))) {
                send(client, 404, "{\"error\":\"Маршрут не найден.\"}", ""); return;
            }
            Map<String, String> headers = new HashMap<>();
            int headerBytes = first.length();
            while (true) {
                String line = readLine(input, 8192);
                if (line == null || line.isEmpty()) break;
                headerBytes += line.length();
                if (headerBytes > 16384) { send(client, 431, "{\"error\":\"Слишком большие заголовки.\"}", ""); return; }
                int colon = line.indexOf(':');
                if (colon > 0) headers.put(line.substring(0, colon).trim().toLowerCase(Locale.ROOT), line.substring(colon + 1).trim());
            }
            String origin = allowedOrigin(headers.get("origin"));
            if (method.equals("OPTIONS")) { send(client, 204, "", origin); return; }
            int length = 0;
            if (headers.containsKey("content-length")) {
                try { length = Integer.parseInt(headers.get("content-length")); }
                catch (Exception error) { send(client, 400, "{\"error\":\"Некорректный размер запроса.\"}", origin); return; }
            }
            if (length < 0 || length > (2 << 20) || headers.containsKey("transfer-encoding")) {
                send(client, 413, "{\"error\":\"Запрос слишком большой.\"}", origin); return;
            }
            byte[] bodyBytes = readExact(input, length);
            if (bodyBytes.length != length) { send(client, 400, "{\"error\":\"Запрос передан не полностью.\"}", origin); return; }
            JSONObject request = new JSONObject();
            request.put("method", method);
            request.put("path", path);
            request.put("authorization", headers.getOrDefault("authorization", ""));
            request.put("contentType", headers.getOrDefault("content-type", ""));
            request.put("body", new String(bodyBytes, StandardCharsets.UTF_8));
            request.put("remote", client.getInetAddress().getHostAddress());
            MainActivity.HostResponse response = activity.dispatchHostRequest(request.toString());
            send(client, response.status, response.body, origin);
        } catch (Exception ignored) {
            // A disconnected client must not affect the authoritative host state.
        }
    }

    private void send(Socket socket, int status, String body, String origin) throws Exception {
        byte[] payload = body == null ? new byte[0] : body.getBytes(StandardCharsets.UTF_8);
        String reason = status == 200 ? "OK" : status == 204 ? "No Content" : status == 400 ? "Bad Request" :
                status == 401 ? "Unauthorized" : status == 403 ? "Forbidden" : status == 404 ? "Not Found" :
                status == 409 ? "Conflict" : status == 423 ? "Locked" : status == 429 ? "Too Many Requests" : "Error";
        StringBuilder header = new StringBuilder("HTTP/1.1 ").append(status).append(' ').append(reason).append("\r\n")
                .append("Content-Type: application/json; charset=utf-8\r\n")
                .append("Cache-Control: no-store\r\n")
                .append("X-Content-Type-Options: nosniff\r\n")
                .append("Connection: close\r\n")
                .append("Access-Control-Allow-Headers: Authorization, Content-Type\r\n")
                .append("Access-Control-Allow-Methods: GET, POST, OPTIONS\r\n")
                .append("Access-Control-Allow-Private-Network: true\r\n");
        if (origin != null && !origin.isEmpty()) header.append("Access-Control-Allow-Origin: ").append(origin).append("\r\nVary: Origin\r\n");
        header.append("Content-Length: ").append(payload.length).append("\r\n\r\n");
        OutputStream output = socket.getOutputStream();
        output.write(header.toString().getBytes(StandardCharsets.US_ASCII));
        output.write(payload);
        output.flush();
    }

    private static String readLine(InputStream input, int limit) throws Exception {
        ByteArrayOutputStream output = new ByteArrayOutputStream();
        while (output.size() <= limit) {
            int value = input.read();
            if (value < 0) return output.size() == 0 ? null : output.toString(StandardCharsets.US_ASCII.name());
            if (value == '\n') break;
            if (value != '\r') output.write(value);
        }
        if (output.size() > limit) throw new IllegalArgumentException("HTTP line too long");
        return output.toString(StandardCharsets.US_ASCII.name());
    }

    private static byte[] readExact(InputStream input, int length) throws Exception {
        byte[] result = new byte[length];
        int offset = 0;
        while (offset < length) {
            int count = input.read(result, offset, length - offset);
            if (count < 0) break;
            offset += count;
        }
        return offset == length ? result : java.util.Arrays.copyOf(result, offset);
    }

    private static final class AddressCandidate {
        final String url;
        final int score;
        AddressCandidate(String url, int score) { this.url = url; this.score = score; }
    }

    private String addressesJson() throws Exception {
        List<AddressCandidate> addresses = new ArrayList<>();
        Enumeration<NetworkInterface> interfaces = NetworkInterface.getNetworkInterfaces();
        for (NetworkInterface network : Collections.list(interfaces)) {
            if (!network.isUp() || network.isLoopback()) continue;
            for (InetAddress address : Collections.list(network.getInetAddresses())) {
                if (address instanceof Inet4Address && !address.isLoopbackAddress() && !address.isLinkLocalAddress())
                    addresses.add(new AddressCandidate(
                            "http://" + address.getHostAddress() + ":" + port,
                            networkAddressScore(network, address)));
            }
        }
        Collections.sort(addresses, (left, right) -> {
            if (left.score != right.score) return Integer.compare(right.score, left.score);
            return left.url.compareTo(right.url);
        });
        org.json.JSONArray result = new org.json.JSONArray();
        for (AddressCandidate address : addresses) result.put(address.url);
        return result.toString();
    }

    private static int networkAddressScore(NetworkInterface network, InetAddress address) {
        String name = ((network.getName() == null ? "" : network.getName()) + " "
                + (network.getDisplayName() == null ? "" : network.getDisplayName())).toLowerCase(Locale.ROOT);
        int score = address.isSiteLocalAddress() ? 20 : 0;
        byte[] raw = address.getAddress();
        if (raw.length == 4 && (raw[0] & 255) == 192 && (raw[1] & 255) == 168) score += 20;
        if (name.contains("wlan") || name.contains("wifi") || name.contains("wi-fi") || name.contains("wireless")) score += 70;
        if (name.startsWith("eth") || name.startsWith("en")) score += 50;
        String[] virtual = {"vpn", "tun", "tap", "virtual", "docker", "tailscale", "zerotier", "wireguard", "rmnet"};
        for (String marker : virtual) {
            if (name.contains(marker)) { score -= 200; break; }
        }
        return score;
    }

    private static String allowedOrigin(String origin) {
        if (origin == null || origin.isEmpty()) return "";
        if (origin.equals("null") || origin.equals("https://appassets.androidplatform.net")) return origin;
        if (!origin.matches("^http://(?:localhost|127(?:\\.\\d{1,3}){3}|10(?:\\.\\d{1,3}){3}|192\\.168(?:\\.\\d{1,3}){2}|172\\.(?:1[6-9]|2\\d|3[01])(?:\\.\\d{1,3}){2})(?::\\d{1,5})?$")) return "";
        return origin;
    }
}
