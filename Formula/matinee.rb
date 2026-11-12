# Homebrew formula written by the release workflow from
# packaging/homebrew/matinee.rb.tmpl. Edit the template, not Formula/matinee.rb.
class Matinee < Formula
  desc "Self-hosted media server for movies, TV and music"
  homepage "https://matinee.yeix.io"
  version "0.2.4"
  license "AGPL-3.0-or-later"

  depends_on "ffmpeg"

  on_macos do
    on_arm do
      url "https://github.com/yeixio/matinee/releases/download/v0.2.4/matinee_0.2.4_darwin_arm64.tar.gz"
      sha256 "41786d907d0b47df98cbe7c53765c72693b924613602c312f0b2cc2a1d92952e"
    end
    on_intel do
      url "https://github.com/yeixio/matinee/releases/download/v0.2.4/matinee_0.2.4_darwin_amd64.tar.gz"
      sha256 "88130b816ad8d125a280af919f3e6edd5642e828b69c63de08ac89b7f0014f43"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/yeixio/matinee/releases/download/v0.2.4/matinee_0.2.4_linux_arm64.tar.gz"
      sha256 "669226fa93060d04384866958b129d67e990f921e82ca3647e5b8a94cd4e1eac"
    end
    on_intel do
      url "https://github.com/yeixio/matinee/releases/download/v0.2.4/matinee_0.2.4_linux_amd64.tar.gz"
      sha256 "4c29657d08db6a21a70affe4266248d60853dfa484e7836dc6d89843401743d3"
    end
  end

  def install
    bin.install "matinee"
  end

  def post_install
    (var/"matinee").mkpath
    env = etc/"matinee/matinee.env"
    return if env.exist?

    env.dirname.mkpath
    env.write <<~EOS
      # Matinee settings, read by `brew services`. After a change:
      #   brew services restart matinee
      # Every setting is listed at https://github.com/yeixio/matinee#configuration

      # TMDB key for posters and descriptions. How to get one:
      # https://github.com/yeixio/matinee#posters-and-descriptions-tmdb-key
      #MATINEE_TMDB_KEY=

      # Listen address.
      #MATINEE_ADDR=:7171
    EOS
  end

  service do
    # Reads the settings file, so changing one doesn't mean editing the service.
    env = etc/"matinee/matinee.env"
    run ["/bin/sh", "-c", "set -a; [ -f '#{env}' ] && . '#{env}'; exec '#{opt_bin}/matinee'"]
    environment_variables MATINEE_DATA_DIR: var/"matinee", MATINEE_MEDIA_ROOT: Dir.home
    keep_alive true
    log_path var/"log/matinee.log"
    error_log_path var/"log/matinee.log"
  end

  def caveats
    <<~EOS
      Start Matinee now and at login:
        brew services start matinee

      Then open http://localhost:7171 to create the admin account and add
      your folders. Settings, such as a TMDB key for posters, go in
        #{etc}/matinee/matinee.env
      and data lives in #{var}/matinee.
    EOS
  end

  test do
    port = free_port
    pid = spawn({ "MATINEE_DATA_DIR" => testpath.to_s, "MATINEE_ADDR" => "127.0.0.1:#{port}" }, bin/"matinee")
    sleep 3
    assert_match "ok", shell_output("curl -fsS http://127.0.0.1:#{port}/healthz")
  ensure
    Process.kill("TERM", pid)
  end
end
