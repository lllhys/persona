package hub

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/nwverse/persona/server/internal/pgdocker"
)

// --- database ---------------------------------------------------------------------------------------------------

func (e *setupEnv) database(ctx context.Context, cfg *setupConfig) error {
	if cfg.DatabaseURL != "" {
		err := e.pingDB(ctx, cfg.DatabaseURL)
		if err != nil && e.containerExists(ctx) {
			// The container we made may just be stopped (Docker Desktop not started at login, say).
			if e.startContainer(ctx, cfg.DatabaseURL) == nil {
				err = e.pingDB(ctx, cfg.DatabaseURL)
			}
		}
		if err != nil {
			return e.fail("database", "hub.json 里的数据库连不上："+err.Error(), "确认 Postgres 在运行、database_url 对，再重跑")
		}
		e.report("database", true, "用 hub.json 里的数据库", "")
		return nil
	}
	switch err := pgdocker.CheckDocker(ctx, e.run, e.lookPath); {
	case errors.Is(err, pgdocker.ErrNoDocker):
		return e.noDocker()
	case errors.Is(err, pgdocker.ErrDockerDenied):
		return e.fail("database", "有 Docker，但当前用户没有权限用它",
			"运行 sudo usermod -aG docker "+e.user+"，退出登录再登录，然后重跑 personad hub setup")
	case err != nil:
		return e.fail("database", "有 Docker，但它没在运行", e.startDockerHint())
	}
	pg := e.pg()
	created := false
	var password string
	if !pg.Exists(ctx) {
		password = randomHex(24)
		var step *pgdocker.StepError
		if err := pg.Create(ctx, password); errors.As(err, &step) && step.Step == "volume" {
			return e.fail("database", "建数据卷 "+e.dbVolume+" 失败："+step.Output, "看 docker 的报错，处理后重跑")
		} else if err != nil {
			why := err.Error()
			if step != nil {
				why = step.Output
			}
			return e.fail("database", "建数据库容器 "+e.dbContainer+" 失败："+why,
				fmt.Sprintf("端口 %d 被占着或拉不到镜像 %s 时处理后重跑", e.dbPort, pgdocker.Image))
		}
		created = true
	} else {
		// The container is there but hub.json lost its address: the password it was made with is in its environment.
		var err error
		if password, err = pg.Password(ctx); err != nil {
			return e.fail("database", "已有数据库容器 "+e.dbContainer+"，但 hub.json 里没有它的地址，也读不出它的密码",
				"把 database_url 写回 hub.json（postgres://persona:<密码>@127.0.0.1:"+strconv.Itoa(e.dbPort)+"/persona?sslmode=disable）再重跑")
		}
	}
	if err := e.startContainer(ctx, ""); err != nil {
		return e.fail("database", "数据库容器 "+e.dbContainer+" 没准备好："+err.Error(), "运行 docker logs "+e.dbContainer+" 看原因")
	}
	port, err := pg.HostPort(ctx)
	if err != nil {
		port = e.dbPort // a container on the host's network (db.sh): its port is the one it was made with
	}
	dbURL := pgdocker.URL(password, port)
	if err := editHubJSON(e.root, func(o *jsonObject) error { return o.Set("database_url", dbURL) }); err != nil {
		return e.fail("database", "写不了 "+e.hubJSON()+"："+err.Error(), "检查这个目录的权限")
	}
	cfg.DatabaseURL = dbURL
	if err := e.pingUntil(ctx, dbURL); err != nil {
		return e.fail("database", "数据库容器起来了，但连不上："+err.Error(), "运行 docker logs "+e.dbContainer+" 看原因")
	}
	if created {
		e.report("database", true, "建好了 Docker 里的 Postgres（容器 "+e.dbContainer+"，数据卷 "+e.dbVolume+"），地址写进了 hub.json", "")
	} else {
		e.report("database", true, "用已有的 Docker 容器 "+e.dbContainer+"，地址写回了 hub.json", "")
	}
	return nil
}

// pingUntil connects to the database, again for a while: the port Docker publishes may lag the server inside.
func (e *setupEnv) pingUntil(ctx context.Context, dbURL string) error {
	deadline := time.Now().Add(e.dbWait)
	for {
		err := e.pingDB(ctx, dbURL)
		if err == nil || !time.Now().Before(deadline) || ctx.Err() != nil {
			return err
		}
		e.sleep(time.Second)
	}
}

func (e *setupEnv) noDocker() error {
	if !e.portFree(5432) {
		return e.fail("database", "没有 Docker；这台电脑的 5432 端口上像是有 Postgres",
			"在它里面建用户 persona 和库 persona，把 postgres://persona:<密码>@127.0.0.1:5432/persona 写进 "+e.hubJSON()+
				" 的 database_url，再重跑（docs/server/server.md「数据库」）；或者装 Docker 后重跑")
	}
	if e.goos == "darwin" {
		return e.fail("database", "这台电脑还缺数据库", "装 Docker Desktop 或 OrbStack 后重跑这条命令")
	}
	return e.fail("database", "这台电脑还缺数据库", "装 Docker（https://docs.docker.com/engine/install/）后重跑这条命令")
}

func (e *setupEnv) startDockerHint() string {
	if e.goos == "darwin" {
		return "打开 Docker Desktop 或 OrbStack，等它启动好后重跑"
	}
	return "运行 sudo systemctl start docker，再重跑"
}

// pg is the database container setup makes (persona-postgres unless a trial run named another).
func (e *setupEnv) pg() *pgdocker.Container {
	return &pgdocker.Container{Name: e.dbContainer, Volume: e.dbVolume, Port: e.dbPort, Run: e.run, Sleep: e.sleep, Wait: e.dbWait}
}

func (e *setupEnv) containerExists(ctx context.Context) bool { return e.pg().Exists(ctx) }

// startContainer starts the container and waits until Postgres in it answers (pgdocker.Container.Start).
func (e *setupEnv) startContainer(ctx context.Context, dbURL string) error {
	return e.pg().Start(ctx, dbURL)
}
