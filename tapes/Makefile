TAPES := $(wildcard scene*.tape)
GIFS  := $(TAPES:.tape=.gif)
CLIPS := $(TAPES:.tape=.mp4)

all: $(GIFS)

demo.mp4: $(CLIPS)
	printf "file '%s'\n" $(sort $(CLIPS)) > concat.txt
	ffmpeg -y -f concat -safe 0 -i concat.txt -c copy $@
	rm concat.txt

%.mp4: %.gif
	ffmpeg -y -i $< -vf "fps=10,scale=1200:-2" $@

%.gif: %.tape
	vhs $<

clean:
	rm -f $(GIFS) $(CLIPS) demo.mp4 concat.txt

.PHONY: all clean
